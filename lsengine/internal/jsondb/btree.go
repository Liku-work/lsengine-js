// internal/jsondb/btree.go
package jsondb

import (
	"sort"
	"sync"
)

type BTreeIndex struct {
	root   *btreeNode
	mu     sync.RWMutex
	degree int
	size   int64
	pool   *sync.Pool
}

type btreeNode struct {
	keys     []interface{}
	values   [][]interface{}
	children []*btreeNode
	leaf     bool
	next     *btreeNode
}

func NewBTreeIndex(degree int) *BTreeIndex {
	return &BTreeIndex{
		root:   &btreeNode{leaf: true, keys: make([]interface{}, 0, degree*2)},
		degree: degree,
		pool: &sync.Pool{
			New: func() interface{} {
				return &btreeNode{
					keys:     make([]interface{}, 0, degree*2),
					values:   make([][]interface{}, 0, degree*2),
					children: make([]*btreeNode, 0, degree*2+1),
				}
			},
		},
	}
}

func (bt *BTreeIndex) compare(a, b interface{}) int {
	switch va := a.(type) {
	case string:
		if vb, ok := b.(string); ok {
			if va < vb {
				return -1
			} else if va > vb {
				return 1
			}
			return 0
		}
	case int64:
		if vb, ok := b.(int64); ok {
			if va < vb {
				return -1
			} else if va > vb {
				return 1
			}
			return 0
		}
	case float64:
		if vb, ok := b.(float64); ok {
			if va < vb {
				return -1
			} else if va > vb {
				return 1
			}
			return 0
		}
	}
	return 0
}

func (bt *BTreeIndex) Insert(key interface{}, value interface{}) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	node := bt.root
	if len(node.keys) == cap(node.keys) {
		newRoot := bt.pool.Get().(*btreeNode)
		newRoot.leaf = false
		newRoot.children = append(newRoot.children, node)
		bt.splitChild(newRoot, 0)
		bt.root = newRoot
		node = newRoot
	}

	bt.insertNonFull(node, key, value)
	bt.size++
}

func (bt *BTreeIndex) insertNonFull(node *btreeNode, key interface{}, value interface{}) {
	if node.leaf {
		pos := sort.Search(len(node.keys), func(j int) bool {
			return bt.compare(node.keys[j], key) >= 0
		})

		node.keys = append(node.keys[:pos], append([]interface{}{key}, node.keys[pos:]...)...)

		values := make([]interface{}, 1)
		values[0] = value
		node.values = append(node.values[:pos], append([][]interface{}{values}, node.values[pos:]...)...)
	} else {
		pos := sort.Search(len(node.keys), func(j int) bool {
			return bt.compare(node.keys[j], key) >= 0
		})

		if pos >= len(node.children) {
			pos = len(node.children) - 1
		}

		if pos < len(node.children) && len(node.children[pos].keys) == cap(node.children[pos].keys) {
			bt.splitChild(node, pos)
			if pos < len(node.keys) && bt.compare(key, node.keys[pos]) > 0 {
				pos++
			}
		}

		if pos >= len(node.children) {
			node.children = append(node.children, bt.pool.Get().(*btreeNode))
		}

		bt.insertNonFull(node.children[pos], key, value)
	}
}

func (bt *BTreeIndex) splitChild(parent *btreeNode, index int) {
	child := parent.children[index]
	mid := bt.degree - 1

	newNode := bt.pool.Get().(*btreeNode)
	newNode.leaf = child.leaf

	newNode.keys = append(newNode.keys, child.keys[mid+1:]...)
	newNode.values = append(newNode.values, child.values[mid+1:]...)

	if !child.leaf {
		newNode.children = append(newNode.children, child.children[mid+1:]...)
	}

	child.keys = child.keys[:mid]
	child.values = child.values[:mid]
	if !child.leaf {
		child.children = child.children[:mid+1]
	}

	parent.keys = append(parent.keys[:index], append([]interface{}{child.keys[mid]}, parent.keys[index:]...)...)
	parent.values = append(parent.values[:index], append([][]interface{}{child.values[mid]}, parent.values[index:]...)...)
	parent.children = append(parent.children[:index+1], append([]*btreeNode{newNode}, parent.children[index+1:]...)...)
}

func (bt *BTreeIndex) Search(key interface{}) []interface{} {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	node := bt.root
	for node != nil {
		idx := sort.Search(len(node.keys), func(i int) bool {
			return bt.compare(key, node.keys[i]) <= 0
		})

		if idx < len(node.keys) && bt.compare(key, node.keys[idx]) == 0 {
			return node.values[idx]
		}

		if node.leaf {
			break
		}
		node = node.children[idx]
	}
	return nil
}