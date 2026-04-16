// internal/jsondb/engine.go
package jsondb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/sync/singleflight"
	"lsengine/internal/config"
	"lsengine/internal/metrics"
)

type JSONDocument struct {
	ID        string                 `json:"id"`
	Data      map[string]interface{} `json:"data"`
	CreatedAt int64                  `json:"createdAt"`
	UpdatedAt int64                  `json:"updatedAt"`
	ShardID   int                    `json:"-"`
}

type PageResult struct {
	Documents []JSONDocument `json:"documents"`
	Page      int            `json:"page"`
	PageSize  int            `json:"pageSize"`
	HasMore   bool           `json:"hasMore"`
	Total     int64          `json:"total,omitempty"`
}

type JSONShard struct {
	ID int
	DB *sql.DB
}

type JSONEngine struct {
	shards          []*JSONShard
	loadGroup       singleflight.Group
	pageSize        int
	queryTimeout    time.Duration
	shardCount      int
	metadataDB      *sql.DB
	collectionCache *CollectionCache
}

type CollectionCache struct {
	cache map[string]*JSONCollection
	mu    sync.RWMutex
}

type JSONCollection struct {
	Name     string
	Schema   map[string]string
	Indices  []string
	docCount int64
	shardKey string
	mu       sync.RWMutex
}

var GlobalJSONEngine *JSONEngine

func InitJSONEngine(dataDir string) error {
	dbPath := filepath.Join(dataDir, "json_meta.db")

	metadataDB, err := sql.Open("sqlite3", dbPath+"?_journal=WAL&_synchronous=NORMAL&_cache_size=10000")
	if err != nil {
		return err
	}

	metadataDB.SetMaxOpenConns(10)
	metadataDB.SetMaxIdleConns(5)
	metadataDB.SetConnMaxLifetime(5 * time.Minute)

	createMetaTables := `
	CREATE TABLE IF NOT EXISTS collections (
		name TEXT PRIMARY KEY,
		schema TEXT,
		indices TEXT,
		document_count INTEGER DEFAULT 0,
		shard_key TEXT,
		created_at INTEGER,
		updated_at INTEGER
	);
	CREATE TABLE IF NOT EXISTS index_stats (
		collection TEXT,
		field TEXT,
		index_type TEXT,
		size INTEGER,
		updated_at INTEGER,
		PRIMARY KEY (collection, field)
	);
	`

	_, err = metadataDB.Exec(createMetaTables)
	if err != nil {
		return err
	}

	shardCount := config.JSON_SHARD_COUNT
	shards := make([]*JSONShard, shardCount)

	for i := 0; i < shardCount; i++ {
		shardPath := filepath.Join(dataDir, fmt.Sprintf("json_shard_%d.db", i))
		db, err := sql.Open("sqlite3", shardPath+"?_journal=WAL&_synchronous=NORMAL&_cache_size=10000")
		if err != nil {
			return err
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
			return err
		}

		shards[i] = &JSONShard{ID: i, DB: db}
	}

	GlobalJSONEngine = &JSONEngine{
		shards:          shards,
		pageSize:        config.JSON_PAGE_SIZE,
		queryTimeout:    5 * time.Second,
		shardCount:      shardCount,
		metadataDB:      metadataDB,
		collectionCache: &CollectionCache{cache: make(map[string]*JSONCollection)},
	}

	rows, err := metadataDB.Query("SELECT name, schema, indices FROM collections")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var name, schemaStr, indicesStr string
		if err := rows.Scan(&name, &schemaStr, &indicesStr); err != nil {
			continue
		}

		var schema map[string]string
		var indices []string

		json.Unmarshal([]byte(schemaStr), &schema)
		json.Unmarshal([]byte(indicesStr), &indices)

		collection := &JSONCollection{
			Name:    name,
			Schema:  schema,
			Indices: indices,
		}

		GlobalJSONEngine.collectionCache.cache[name] = collection
	}

	return nil
}

func (je *JSONEngine) getShard(id string, collection string) int {
	h := fnv.New32a()
	h.Write([]byte(collection + ":" + id))
	return int(h.Sum32()) % je.shardCount
}

func (je *JSONEngine) getCollection(name string) (*JSONCollection, error) {
	je.collectionCache.mu.RLock()
	if cached, ok := je.collectionCache.cache[name]; ok {
		je.collectionCache.mu.RUnlock()
		return cached, nil
	}
	je.collectionCache.mu.RUnlock()

	result, err, _ := je.loadGroup.Do("col:"+name, func() (interface{}, error) {
		var schemaStr, indicesStr, shardKey string
		var docCount int64

		err := je.metadataDB.QueryRow(`
			SELECT schema, indices, document_count, shard_key 
			FROM collections WHERE name = ?
		`, name).Scan(&schemaStr, &indicesStr, &docCount, &shardKey)

		if err != nil {
			if err == sql.ErrNoRows {
				return nil, fmt.Errorf("collection not found")
			}
			return nil, err
		}

		var schema map[string]string
		var indices []string
		if err := json.Unmarshal([]byte(schemaStr), &schema); err != nil {
			schema = make(map[string]string)
		}
		if err := json.Unmarshal([]byte(indicesStr), &indices); err != nil {
			indices = []string{}
		}

		collection := &JSONCollection{
			Name:     name,
			Schema:   schema,
			Indices:  indices,
			docCount: docCount,
			shardKey: shardKey,
		}

		je.collectionCache.mu.Lock()
		je.collectionCache.cache[name] = collection
		je.collectionCache.mu.Unlock()

		return collection, nil
	})

	if err != nil {
		return nil, err
	}
	return result.(*JSONCollection), nil
}

func (je *JSONEngine) CreateCollection(name string, schema map[string]string, indices []string) error {
	atomic.AddInt64(&metrics.GlobalMetrics.JsonOps, 1)

	var exists int
	err := je.metadataDB.QueryRow("SELECT COUNT(*) FROM collections WHERE name = ?", name).Scan(&exists)
	if err != nil {
		atomic.AddInt64(&metrics.GlobalMetrics.JsonErrors, 1)
		return err
	}
	if exists > 0 {
		return fmt.Errorf("collection already exists")
	}

	schemaJSON, _ := json.Marshal(schema)
	indicesJSON, _ := json.Marshal(indices)

	now := time.Now().Unix()
	_, err = je.metadataDB.Exec(
		"INSERT INTO collections (name, schema, indices, document_count, created_at, updated_at) VALUES (?, ?, ?, 0, ?, ?)",
		name, string(schemaJSON), string(indicesJSON), now, now,
	)
	if err != nil {
		atomic.AddInt64(&metrics.GlobalMetrics.JsonErrors, 1)
		return err
	}

	collection := &JSONCollection{
		Name:    name,
		Schema:  schema,
		Indices: indices,
	}
	je.collectionCache.mu.Lock()
	je.collectionCache.cache[name] = collection
	je.collectionCache.mu.Unlock()

	return nil
}

func (je *JSONEngine) Insert(collection string, doc map[string]interface{}) (string, error) {
	atomic.AddInt64(&metrics.GlobalMetrics.JsonOps, 1)

	col, err := je.getCollection(collection)
	if err != nil {
		atomic.AddInt64(&metrics.GlobalMetrics.JsonErrors, 1)
		return "", err
	}

	// Validate schema
	for field, fieldType := range col.Schema {
		if val, exists := doc[field]; exists {
			switch fieldType {
			case "string":
				if _, ok := val.(string); !ok {
					return "", fmt.Errorf("field %s must be string", field)
				}
			case "number":
				switch val.(type) {
				case float64, int64, int:
				default:
					return "", fmt.Errorf("field %s must be number", field)
				}
			case "boolean":
				if _, ok := val.(bool); !ok {
					return "", fmt.Errorf("field %s must be boolean", field)
				}
			}
		}
	}

	id := fmt.Sprintf("%s_%d_%d", collection, time.Now().UnixNano(), rand.Intn(1000))
	now := time.Now().Unix()

	dataJSON, _ := json.Marshal(doc)
	shardID := je.getShard(id, collection)
	shard := je.shards[shardID]

	_, err = shard.DB.Exec(
		"INSERT INTO documents (id, collection, data, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		id, collection, string(dataJSON), now, now,
	)
	if err != nil {
		atomic.AddInt64(&metrics.GlobalMetrics.JsonErrors, 1)
		return "", err
	}

	go func() {
		je.metadataDB.Exec("UPDATE collections SET document_count = document_count + 1, updated_at = ? WHERE name = ?", now, collection)
	}()

	return id, nil
}

func (je *JSONEngine) FindPage(collection string, query map[string]interface{}, page, pageSize int) (*PageResult, error) {
	if pageSize <= 0 || pageSize > 1000 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}

	col, err := je.getCollection(collection)
	if err != nil {
		return nil, err
	}

	shardsNeeded := (offset + pageSize + je.pageSize - 1) / je.pageSize
	if shardsNeeded > je.shardCount {
		shardsNeeded = je.shardCount
	}

	type shardResult struct {
		shardID int
		docs    []JSONDocument
		err     error
	}

	resultCh := make(chan shardResult, shardsNeeded)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < shardsNeeded; i++ {
		wg.Add(1)
		go func(shardID int) {
			defer wg.Done()

			shard := je.shards[shardID]

			shardOffset := offset / je.shardCount
			if offset%je.shardCount > shardID {
				shardOffset++
			}

			shardLimit := (pageSize / je.shardCount) + 1

			rows, err := shard.DB.QueryContext(ctx, `
				SELECT id, data, created_at, updated_at FROM documents 
				WHERE collection = ? 
				ORDER BY updated_at DESC LIMIT ? OFFSET ?
			`, col.Name, shardLimit, shardOffset)

			if err != nil {
				resultCh <- shardResult{shardID: shardID, err: err}
				return
			}
			defer rows.Close()

			docs := make([]JSONDocument, 0, shardLimit)
			for rows.Next() {
				var doc JSONDocument
				var dataStr string
				if err := rows.Scan(&doc.ID, &dataStr, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
					continue
				}
				json.Unmarshal([]byte(dataStr), &doc.Data)
				doc.ShardID = shardID
				docs = append(docs, doc)
			}

			resultCh <- shardResult{shardID: shardID, docs: docs}
		}(i)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	allDocs := make([]JSONDocument, 0, pageSize)
	for res := range resultCh {
		if res.err != nil {
			continue
		}
		allDocs = append(allDocs, res.docs...)
	}

	sort.Slice(allDocs, func(i, j int) bool {
		return allDocs[i].UpdatedAt > allDocs[j].UpdatedAt
	})

	if len(allDocs) > pageSize {
		allDocs = allDocs[:pageSize]
	}

	return &PageResult{
		Documents: allDocs,
		Page:      offset/pageSize + 1,
		PageSize:  pageSize,
		HasMore:   len(allDocs) == pageSize,
	}, nil
}

func (je *JSONEngine) Find(collection string, query map[string]interface{}) ([]JSONDocument, error) {
	result, err := je.FindPage(collection, query, 1, 10000)
	if err != nil {
		return nil, err
	}
	return result.Documents, nil
}

func (je *JSONEngine) FindOne(collection string, query map[string]interface{}) (*JSONDocument, error) {
	result, err := je.FindPage(collection, query, 1, 1)
	if err != nil {
		return nil, err
	}
	if len(result.Documents) == 0 {
		return nil, nil
	}
	return &result.Documents[0], nil
}

func (je *JSONEngine) Update(collection, id string, update map[string]interface{}) error {
	atomic.AddInt64(&metrics.GlobalMetrics.JsonOps, 1)

	shardID := je.getShard(id, collection)
	shard := je.shards[shardID]

	var dataStr string
	err := shard.DB.QueryRow("SELECT data FROM documents WHERE id = ?", id).Scan(&dataStr)
	if err != nil {
		atomic.AddInt64(&metrics.GlobalMetrics.JsonErrors, 1)
		return err
	}

	var doc map[string]interface{}
	json.Unmarshal([]byte(dataStr), &doc)

	for k, v := range update {
		doc[k] = v
	}

	now := time.Now().Unix()
	newData, _ := json.Marshal(doc)

	_, err = shard.DB.Exec(
		"UPDATE documents SET data = ?, updated_at = ? WHERE id = ?",
		string(newData), now, id,
	)
	if err != nil {
		atomic.AddInt64(&metrics.GlobalMetrics.JsonErrors, 1)
		return err
	}

	return nil
}

func (je *JSONEngine) Delete(collection, id string) error {
	atomic.AddInt64(&metrics.GlobalMetrics.JsonOps, 1)

	shardID := je.getShard(id, collection)
	shard := je.shards[shardID]

	_, err := shard.DB.Exec("DELETE FROM documents WHERE id = ?", id)
	if err != nil {
		atomic.AddInt64(&metrics.GlobalMetrics.JsonErrors, 1)
		return err
	}

	go func() {
		je.metadataDB.Exec("UPDATE collections SET document_count = document_count - 1, updated_at = ? WHERE name = ?", time.Now().Unix(), collection)
	}()

	return nil
}

func (je *JSONEngine) Close() error {
	for _, shard := range je.shards {
		if shard.DB != nil {
			shard.DB.Close()
		}
	}
	if je.metadataDB != nil {
		return je.metadataDB.Close()
	}
	return nil
}