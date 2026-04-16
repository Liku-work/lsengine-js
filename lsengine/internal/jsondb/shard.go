// internal/jsondb/shard.go
package jsondb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"lsengine/internal/config"
)

type JSONShardManager struct {
	shards     []*JSONShard
	shardCount int
	mu         sync.RWMutex
}

func NewJSONShardManager(dataDir string, shardCount int) (*JSONShardManager, error) {
	shards := make([]*JSONShard, shardCount)

	for i := 0; i < shardCount; i++ {
		shardPath := filepath.Join(dataDir, fmt.Sprintf("json_shard_%d.db", i))
		db, err := sql.Open("sqlite3", shardPath+"?_journal=WAL&_synchronous=NORMAL&_cache_size=10000")
		if err != nil {
			return nil, err
		}

		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(10)
		db.SetConnMaxLifetime(5 * time.Minute)

		createShardTables := `
		CREATE TABLE IF NOT EXISTS documents (
			id TEXT PRIMARY KEY,
			collection TEXT,
			data TEXT,
			created_at INTEGER,
			updated_at INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_docs_collection ON documents(collection);
		CREATE INDEX IF NOT EXISTS idx_docs_updated ON documents(updated_at);
		`

		_, err = db.Exec(createShardTables)
		if err != nil {
			return nil, err
		}

		shards[i] = &JSONShard{ID: i, DB: db}
	}

	return &JSONShardManager{
		shards:     shards,
		shardCount: shardCount,
	}, nil
}

func (sm *JSONShardManager) GetShard(id int) *JSONShard {
	if id < 0 || id >= sm.shardCount {
		return nil
	}
	return sm.shards[id]
}

func (sm *JSONShardManager) GetAllShards() []*JSONShard {
	return sm.shards
}

func (sm *JSONShardManager) Close() {
	for _, shard := range sm.shards {
		if shard.DB != nil {
			shard.DB.Close()
		}
	}
}

func (je *JSONEngine) BulkInsert(collection string, docs []map[string]interface{}) ([]string, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	batchSize := config.BATCH_INSERT_SIZE
	ids := make([]string, 0, len(docs))

	for i := 0; i < len(docs); i += batchSize {
		end := i + batchSize
		if end > len(docs) {
			end = len(docs)
		}

		batchIDs, err := je.bulkInsertBatch(collection, docs[i:end], i)
		if err != nil {
			return ids, err
		}
		ids = append(ids, batchIDs...)
	}

	return ids, nil
}

func (je *JSONEngine) bulkInsertBatch(collection string, docs []map[string]interface{}, baseIndex int) ([]string, error) {
	_, err := je.getCollection(collection)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	shardBatches := make(map[int][][]interface{})
	ids := make([]string, len(docs))

	for idx, doc := range docs {
		id := fmt.Sprintf("%s_%d_%d", collection, now, baseIndex+idx)
		ids[idx] = id

		dataJSON, _ := json.Marshal(doc)
		shardID := je.getShard(id, collection)

		shardBatches[shardID] = append(shardBatches[shardID], []interface{}{
			id, collection, string(dataJSON), now, now,
		})
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(shardBatches))

	for shardID, batch := range shardBatches {
		wg.Add(1)
		go func(sid int, data [][]interface{}) {
			defer wg.Done()

			shard := je.shards[sid]

			tx, err := shard.DB.Begin()
			if err != nil {
				errCh <- err
				return
			}
			defer tx.Rollback()

			stmt, err := tx.Prepare(`INSERT OR REPLACE INTO documents 
				(id, collection, data, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`)
			if err != nil {
				errCh <- err
				return
			}
			defer stmt.Close()

			for _, args := range data {
				if _, err := stmt.Exec(args...); err != nil {
					errCh <- err
					return
				}
			}

			if err := tx.Commit(); err != nil {
				errCh <- err
				return
			}
		}(shardID, batch)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return nil, err
		}
	}

	go func() {
		je.metadataDB.Exec(`
			UPDATE collections SET document_count = document_count + ?, updated_at = ? 
			WHERE name = ?
		`, len(docs), now, collection)
	}()

	return ids, nil
}

func (je *JSONEngine) Query(collection string, conditions map[string]interface{}) ([]JSONDocument, error) {
	return je.Find(collection, conditions)
}