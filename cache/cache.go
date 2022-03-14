package cache

import (
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const (
	// NoExpiration 未过期标志
	NoExpiration time.Duration = -1

	// DefaultExpiration 默认过期时间
	DefaultExpiration time.Duration = 0
)

type Cache struct {
	defaultExpiration time.Duration
	items             map[string]Item // 缓存数据项存储在map中
	mu                sync.RWMutex    // 读写锁
	gcInterval        time.Duration   // 过期数据项清理周期
	stopGc            chan bool
}

// 过期缓存数据项清理
func (c *Cache) gcLoop() {
	ticker := time.NewTicker(c.gcInterval)
	for {
		select {
		case <-ticker.C:
			c.DeleteExpired()
		case <-c.stopGc:
			ticker.Stop()
			return
		}
	}
}

// 删除缓存数据项
func (c *Cache) delete(k string) {
	delete(c.items, k)
}

// DeleteExpired 删除过期数据项
func (c *Cache) DeleteExpired() {
	now := time.Now().UnixNano()
	c.mu.Lock()
	defer c.mu.Unlock()

	for k, v := range c.items {
		if v.Expiration > 0 && now > v.Expiration {
			c.delete(k)
		}
	}
}

/*
实现缓存系统的CRUD接口
*/

// Set 设置缓存数据项，如果数据项存在则覆盖
func (c *Cache) Set(k string, v interface{}, d time.Duration) {
	var e int64
	if d == DefaultExpiration {
		d = c.defaultExpiration
	}

	if d > 0 {
		e = time.Now().Add(d).UnixNano()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[k] = Item{
		Object:     v,
		Expiration: e,
	}
}

// 设置数据项，没有锁操作
func (c *Cache) set(k string, v interface{}, d time.Duration) {
	var e int64
	if d == DefaultExpiration {
		d = c.defaultExpiration
	}

	if d > 0 {
		e = time.Now().Add(d).UnixNano()
	}

	c.items[k] = Item{
		Object:     v,
		Expiration: e,
	}
}

// 获取数据项，如果找到数据项，还需要判断数据项是够已经过期
func (c *Cache) get(k string) (interface{}, bool) {
	item, found := c.items[k]
	if !found {
		return nil, false
	}

	if item.Expired() {
		return nil, false
	}

	return item.Object, true
}

// Add 添加数据项，如果数据项已经存在，则返回错误
func (c *Cache) Add(k string, v interface{}, d time.Duration) error {
	c.mu.Lock()
	_, found := c.get(k)

	if found {
		c.mu.Unlock()
		return fmt.Errorf("item %s already exists", k)
	}

	c.set(k, v, d)
	c.mu.Unlock()
	return nil
}

// Get 获取数据项
func (c *Cache) Get(k string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, found := c.items[k]
	if !found {
		return nil, false
	}

	if item.Expired() {
		return nil, false
	}

	return item.Object, true
}

// Replace 替换一个存在的数据项
func (c *Cache) Replace(k string, v interface{}, d time.Duration) error {
	c.mu.Lock()
	_, found := c.get(k)
	if !found {
		c.mu.Unlock()
		return fmt.Errorf("item %s doesn't exist", k)
	}

	c.set(k, v, d)
	c.mu.Unlock()

	return nil
}

// Delete 删除一个数据项
func (c *Cache) Delete(k string) {
	c.mu.Lock()
	c.delete(k)
	c.mu.Unlock()
}

// Save 将缓存数据项写入到 io.Writer中
func (c *Cache) Save(w io.Writer) (err error) {
	enc := gob.NewEncoder(w)
	defer func() {
		if x := recover(); x != nil {
			err = fmt.Errorf("error registering item types with gob library")
		}
	}()

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, v := range c.items {
		gob.Register(v.Object)
	}
	err = enc.Encode(&c.items)
	return

}

// SaveToFile 保存数据项到文件中
func (c *Cache) SaveToFile(file string) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}

	if err = c.Save(f); err != nil {
		f.Close()
		return err
	}

	return f.Close()
}

// Load 从io.Reader中读取数据项
func (c *Cache) Load(r io.Reader) error {
	dec := gob.NewDecoder(r)
	items := map[string]Item{}
	err := dec.Decode(&items)
	if err == nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		for k, v := range items {
			ov, found := c.items[k]
			if !found || ov.Expired() {
				c.items[k] = v
			}
		}
	}
	return err
}

// LoadFile 从文件中加载缓存数据项
func (c *Cache) LoadFile(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}

	if err = c.Load(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Count 返回缓存数据项的数量
func (c *Cache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Flush 清空缓存
func (c *Cache) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = map[string]Item{}
}

// StopGc 停止过期缓存清理
func (c *Cache) StopGc() {
	c.stopGc <- true
}

// NewCache 创建一个缓存系统
func NewCache(defaultExpiration, gcInterval time.Duration) *Cache {
	c := &Cache{
		defaultExpiration: defaultExpiration,
		items:             map[string]Item{},
		mu:                sync.RWMutex{},
		gcInterval:        gcInterval,
		stopGc:            make(chan bool),
	}

	// 开始启动过期清理goroutine
	go c.gcLoop()
	return c
}