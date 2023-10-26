/*
*
一致性哈希将所有可能的哈希值和节点都映射到一个环形的地址空间上。
每个节点负责环上自己哈希值到下一个节点哈希值之间的区间。
当需要查找一个键时，可以计算该键的哈希值，然后在环上找到该哈希值对应的区间，从而找到负责该键的节点
用于确定给定键应存储在哪个节点上，以实现负载均衡和数据分布的均匀性。
*/
package consistenthash

import (
	"hash/crc32"
	"sort"
	"strconv"
)

// Hash maps bytes to uint32
// 定义一个名为 Hash 的类型，它是一个函数，接受一个字节切片并返回一个 uint32 类型的哈希值
type Hash func(data []byte) uint32

// Map constains all hashed keys
type Map struct {
	hash     Hash
	replicas int            //虚拟节点的数量
	keys     []int          // Sorted
	hashMap  map[int]string //存储哈希键和对应的节点（或键）的映射
}

// New creates a Map instance 创建新的一致性哈希 Map
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
// 添加键到哈希 Map。每个键都会根据虚拟节点的数量被哈希多次，并存储在 keys 切片和 hashMap 中
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

// Get gets the closest item in the hash to the provided key.
// 给定一个键，该函数会返回最近的节点。它首先计算给定键的哈希值，然后在 keys 切片中使用二分搜索找到最近的哈希键，最后返回该哈希键对应的节点
func (m *Map) Get(key string) string {
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
