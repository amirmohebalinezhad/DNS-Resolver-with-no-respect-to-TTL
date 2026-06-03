package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

const bboltBucket = "cache_entries"

type BboltPersistence struct {
	db *bolt.DB
}

func OpenBbolt(path string) (*BboltPersistence, error) {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}

	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	p := &BboltPersistence{db: db}
	if err := p.ensureBucket(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return p, nil
}

func (p *BboltPersistence) ensureBucket() error {
	return p.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bboltBucket))
		return err
	})
}

func (p *BboltPersistence) Load(now time.Time) ([]*Entry, error) {
	var entries []*Entry
	err := p.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(bboltBucket))
		if err != nil {
			return err
		}
		cursor := bucket.Cursor()
		for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
			var entry Entry
			if err := json.Unmarshal(value, &entry); err != nil {
				if err := cursor.Delete(); err != nil {
					return err
				}
				continue
			}
			if entry.Key == "" {
				entry.Key = string(key)
			}
			if !entry.StaleUntil.After(now) {
				if err := cursor.Delete(); err != nil {
					return err
				}
				continue
			}
			entries = append(entries, entry.Clone())
		}
		return nil
	})
	return entries, err
}

func (p *BboltPersistence) Save(entry *Entry) error {
	if entry == nil || entry.Key == "" {
		return nil
	}
	clone := entry.Clone()
	return p.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(bboltBucket))
		if err != nil {
			return err
		}
		value, err := json.Marshal(clone)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(clone.Key), value)
	})
}

func (p *BboltPersistence) Delete(key string) error {
	return p.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(bboltBucket))
		if err != nil {
			return err
		}
		return bucket.Delete([]byte(key))
	})
}

func (p *BboltPersistence) Flush() error {
	return p.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket([]byte(bboltBucket)); err != nil && err != bolt.ErrBucketNotFound {
			return err
		}
		_, err := tx.CreateBucket([]byte(bboltBucket))
		return err
	})
}

func (p *BboltPersistence) Close() error {
	if p == nil || p.db == nil {
		return nil
	}
	return p.db.Close()
}
