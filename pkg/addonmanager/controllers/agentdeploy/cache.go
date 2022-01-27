package agentdeploy

import (
	"crypto/md5"
	"fmt"
	"io"

	"k8s.io/klog/v2"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

type workKey struct {
	cluster string
	name    string
}

type cachedResource struct {
	resourceHash string
	generation   int64
}

type workCache struct {
	cache map[workKey]cachedResource
}

func newWorkCache() *workCache {
	return &workCache{
		cache: map[workKey]cachedResource{},
	}
}

func (w *workCache) updateCache(required, existing *workapiv1.ManifestWork) {
	if required == nil || existing == nil {
		return
	}

	key := workKey{
		cluster: required.Namespace,
		name:    required.Name,
	}

	value := cachedResource{
		resourceHash: hashOfResourceStruct(required),
		generation:   existing.Generation,
	}

	w.cache[key] = value
}

func (w *workCache) removeCache(name, cluster string) {
	key := workKey{
		cluster: cluster,
		name:    name,
	}

	delete(w.cache, key)
}

func (w *workCache) safeToSkipApply(required, existing *workapiv1.ManifestWork) bool {
	if required == nil || existing == nil {
		return false
	}

	cacheKey := workKey{
		cluster: required.Namespace,
		name:    required.Name,
	}

	resourceHash := hashOfResourceStruct(required)

	generation := existing.Generation

	var generationMatch, hashMatch bool
	if cached, exists := w.cache[cacheKey]; exists {
		generationMatch = cached.generation == generation
		hashMatch = cached.resourceHash == resourceHash
		if generationMatch && hashMatch {
			klog.V(4).Infof("found matching generation & manifest hash")
			return true
		}
	}

	return false
}

func hashOfResourceStruct(o interface{}) string {
	oString := fmt.Sprintf("%v", o)
	h := md5.New()
	io.WriteString(h, oString)
	rval := fmt.Sprintf("%x", h.Sum(nil))
	return rval
}
