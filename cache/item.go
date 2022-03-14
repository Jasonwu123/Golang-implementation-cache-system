package cache

import "time"

type Item struct {
	Object     interface{} // 数据项
	Expiration int64       // 过期时间
}

// Expired 判断数据项是否过期
func (i *Item) Expired() bool {
	if i.Expiration == 0 {
		return false
	}

	return time.Now().UnixNano() > i.Expiration
}
