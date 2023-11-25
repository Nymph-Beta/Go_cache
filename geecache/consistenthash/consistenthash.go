package consistenthash

import (
	"hash/crc32"
	"log"
	"sort"
	"strconv"
	"strings"
)

// Hash maps bytes to uint32
type Hash func(data []byte) uint32

// Map constains all hashed keys
type Map struct {
	hash     Hash
	replicas int
	keys     []int // Sorted
	hashMap  map[int]string
}

// New creates a Map instance
func New(replicas int, fn Hash) *Map {
	m := &Map{
		replicas: replicas,
		hash:     fn,
		hashMap:  make(map[int]string),
	}
	if m.hash == nil {
		m.hash = crc32.ChecksumIEEE
	}
	return m
}

// Add adds some keys to the hash.
func (m *Map) Add(keys ...string) {
	for _, key := range keys {
		for i := 0; i < m.replicas; i++ {
			hash := int(m.hash([]byte(strconv.Itoa(i) + key)))
			m.keys = append(m.keys, hash)
			m.hashMap[hash] = key
		}
	}
	sort.Ints(m.keys)
}

// GetforKey gets the closest item in the hash to the provided key.
func (m *Map) GetforKey(key string) string {
	if len(m.keys) == 0 {
		return ""
	}

	hash := int(m.hash([]byte(key)))
	// Binary search for appropriate replica.
	idx := sort.Search(len(m.keys), func(i int) bool {
		return m.keys[i] >= hash
	})

	return m.hashMap[m.keys[idx%len(m.keys)]]
}

// Get gets items from peer
func (m *Map) Get(peeraddr string) (string, string) {
	// 使用 strings.LastIndex 查找最后一个冒号 (:) 的位置
	colonIndex := strings.LastIndex(peeraddr, ":")

	// 获取冒号之后的子串，即端口号
	nowport := ""
	if colonIndex != -1 && colonIndex < len(peeraddr)-1 {
		nowport = peeraddr[colonIndex+1:]
	}

	log.Println("now port:", nowport)
	url := peeraddr + "/"
	//peer := &httpGetter{baseURL: url}

	return url, nowport
}
