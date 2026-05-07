package store

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
)

func TestBasic(t *testing.T) {
	caches := NewExecutorCache()
	count := 100
	wg := sync.WaitGroup{}
	wg.Add(count)
	for i := 0; i < 10; i++ {
		for j := 0; j < 10; j++ {
			go func(i, j int) {
				defer wg.Done()
				caches.Upsert(fmt.Sprintf("%v", i), Dimension{
					Name: fmt.Sprintf("%v", j),
				}, nil)
			}(i, j)
		}
	}
	wg.Wait()

	countResult := caches.Count()
	if countResult != count {
		t.Errorf("Expected count %d but got %d", count, countResult)
	}

	necessaryCaches := NewExecutorCache()
	wg.Add(50)
	for i := 0; i < 5; i++ {
		for j := 0; j < 10; j++ {
			go func(i, j int) {
				defer wg.Done()
				necessaryCaches.Upsert(fmt.Sprintf("%v", i), Dimension{
					Name: fmt.Sprintf("%v", j),
				}, nil)
			}(i, j)
		}
	}
	wg.Wait()

	caches.CleanupUnnecessaryCaches(necessaryCaches)
	countResult = caches.Count()
	if countResult != 50 {
		t.Errorf("Expected count after cleanup 50 but got %d", countResult)
	}

	exist := caches.DimensionCachesExists(fmt.Sprintf("%v", 0))
	if !exist {
		t.Errorf("Expected dimension 0 exists but got %v", exist)
	}

	exist = caches.DimensionCachesExists(fmt.Sprintf("%v", 6))
	if exist {
		t.Errorf("Expected dimension 6 does not exist but got %v", exist)
	}

	allowed, ok := caches.Get(fmt.Sprintf("%v", 0), Dimension{
		Name: fmt.Sprintf("%v", 0),
	})
	if !ok {
		t.Errorf("Expected executor 0 dimension 0 should be exist but got %v", ok)
	}
	if allowed != nil {
		t.Errorf("Expected executor 0 dimension 0 should be nil but got %v", allowed)
	}

	executor := fmt.Sprintf("%v", 0)
	for j := 0; j < 10; j++ {
		d := Dimension{Name: fmt.Sprintf("%v", j)}
		caches.removeByHash(executor, d.Hash())
	}

	exist = caches.DimensionCachesExists(executor)
	if exist {
		t.Errorf("Expected dimension 0 does not exist but got %v", exist)
	}

	executor = fmt.Sprintf("%v", 1)
	dimensionNameAccumulate := 0
	caches.IterateCacheItems(executor, func(v CacheValue) error {
		dn, err := strconv.Atoi(v.Dimension.Name)
		if err != nil {
			return err
		}
		dimensionNameAccumulate += dn
		return nil
	})

	if dimensionNameAccumulate != 45 {
		t.Errorf("Expected dimension name joining result 45 but got %v", dimensionNameAccumulate)
	}
}

func TestConcurrentRemoveAndGet(t *testing.T) {
	caches := NewExecutorCache()
	executor := "test-executor"

	for i := 0; i < 100; i++ {
		d := Dimension{Name: fmt.Sprintf("%d", i)}
		caches.Upsert(executor, d, nil)
	}

	wg := sync.WaitGroup{}
	wg.Add(200)
	for i := 0; i < 100; i++ {
		go func(i int) {
			defer wg.Done()
			d := Dimension{Name: fmt.Sprintf("%d", i)}
			caches.removeByHash(executor, d.Hash())
		}(i)
		go func(i int) {
			defer wg.Done()
			d := Dimension{Name: fmt.Sprintf("%d", i)}
			caches.Get(executor, d)
		}(i)
	}
	wg.Wait()

	if caches.Count() != 0 {
		t.Errorf("Expected all items removed but got %d", caches.Count())
	}
}

func TestConcurrentGetCacheItemsWhileModifying(t *testing.T) {
	caches := NewExecutorCache()

	for i := 0; i < 10; i++ {
		for j := 0; j < 10; j++ {
			caches.Upsert(fmt.Sprintf("%d", i), Dimension{Name: fmt.Sprintf("%d", j)}, nil)
		}
	}

	wg := sync.WaitGroup{}
	wg.Add(30)

	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			caches.Count()
		}()
	}

	for i := 0; i < 10; i++ {
		go func(i int) {
			defer wg.Done()
			caches.Upsert(fmt.Sprintf("new-%d", i), Dimension{Name: "test"}, nil)
		}(i)
	}

	for i := 0; i < 10; i++ {
		go func(i int) {
			defer wg.Done()
			d := Dimension{Name: fmt.Sprintf("%d", 0)}
			caches.removeByHash(fmt.Sprintf("%d", i), d.Hash())
		}(i)
	}

	wg.Wait()
}

func TestConcurrentCleanupUnnecessaryCaches(t *testing.T) {
	caches := NewExecutorCache()

	for i := 0; i < 20; i++ {
		for j := 0; j < 10; j++ {
			caches.Upsert(fmt.Sprintf("%d", i), Dimension{Name: fmt.Sprintf("%d", j)}, nil)
		}
	}

	necessaryCaches := NewExecutorCache()
	for i := 0; i < 10; i++ {
		for j := 0; j < 5; j++ {
			necessaryCaches.Upsert(fmt.Sprintf("%d", i), Dimension{Name: fmt.Sprintf("%d", j)}, nil)
		}
	}

	wg := sync.WaitGroup{}
	wg.Add(11)

	go func() {
		defer wg.Done()
		caches.CleanupUnnecessaryCaches(necessaryCaches)
	}()

	for i := 0; i < 10; i++ {
		go func(i int) {
			defer wg.Done()
			executor := fmt.Sprintf("%d", i)
			d := Dimension{Name: fmt.Sprintf("%d", i)}
			caches.Get(executor, d)
		}(i)
	}

	wg.Wait()
}
