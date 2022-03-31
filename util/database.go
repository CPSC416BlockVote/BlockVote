package util

import (
	"errors"
	"github.com/dgraph-io/badger/v3"
	"os"
)

type Database struct {
	instance *badger.DB
}

func (db *Database) Opened() bool {
	return db.instance != nil
}

func (db *Database) KeyExist(key []byte) (found bool) {
	_ = db.instance.View(func(txn *badger.Txn) error {
		_, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			found = false
		} else if err != nil {
			return err
		}
		found = true
		return nil
	})
	return found
}

func (db *Database) Put(key []byte, value []byte) error {
	if !db.Opened() {
		return errors.New("no database instance has been created")
	}

	err := db.instance.Update(func(txn *badger.Txn) error {
		err := txn.Set(key, value)
		if err != nil {
			return err
		}
		return nil
	})
	return err
}

func (db *Database) PutMulti(keys [][]byte, values [][]byte) error {
	if !db.Opened() {
		return errors.New("no database instance has been created")
	}
	if len(keys) != len(values) {
		return errors.New("length of keys is not equal to length of values")
	}

	err := db.instance.Update(func(txn *badger.Txn) error {
		for idx, _ := range keys {
			err := txn.Set(keys[idx], values[idx])
			if err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func (db *Database) Get(key []byte) ([]byte, error) {
	var valCopy []byte
	err := db.instance.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		err = item.Value(func(val []byte) error {
			// This func with val would only be called if item.Value encounters no error.
			// Copying or parsing val is valid.
			valCopy = append([]byte{}, val...)
			return nil
		})
		if err != nil {
			return err
		}

		return nil
	})
	return valCopy, err
}

func (db *Database) GetMulti(keys [][]byte) ([][]byte, error) {
	var valCopy [][]byte
	err := db.instance.View(func(txn *badger.Txn) error {
		for _, key := range keys {
			item, err := txn.Get(key)
			if err != nil {
				return err
			}

			err = item.Value(func(val []byte) error {
				// This func with val would only be called if item.Value encounters no error.
				// Copying or parsing val is valid.
				valCopy = append(valCopy, append([]byte{}, val...))
				return nil
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
	return valCopy, err
}

func (db *Database) GetAllWithPrefix(prefix string) (values [][]byte, err error) {
	err = db.instance.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte(prefix)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			_ = item.Value(func(v []byte) error {
				values = append(values, append([]byte{}, v...))
				return nil
			})
		}
		return nil
	})
	return values, err
}

func (db *Database) New(dbPath string, inMemory bool) error {
	if db.Opened() {
		return errors.New("database instance already created")
	}

	var opt badger.Options
	if !inMemory {
		// check existing database
		if _, err := os.Stat(dbPath); err == nil {
			return errors.New("found existing database")
		}

		opt = badger.DefaultOptions(dbPath)
	} else {
		// disk-less mode
		opt = badger.DefaultOptions("").WithInMemory(true)
	}
	// open database
	instance, err := badger.Open(opt)
	if err != nil {
		return err
	}
	db.instance = instance
	return nil
}

func (db *Database) Load(dbPath string) error {
	if db.Opened() {
		return errors.New("database instance already created")
	}
	// check database path
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return errors.New("database not found")
	}
	// open database
	instance, err := badger.Open(badger.DefaultOptions(dbPath))
	if err != nil {
		return err
	}
	db.instance = instance
	return nil
}

func (db *Database) Close() {
	db.instance.Close()
	db.instance = nil
}
