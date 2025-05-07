package library

import (
	"math"

	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// CostEstimator implements CEL's interpretable.ActualCostEstimator for runtime cost estimation
type CostEstimator struct{}

func actualSize(value ref.Val) uint64 {
	if sz, ok := value.(traits.Sizer); ok {
		return uint64(sz.Size().(types.Int))
	}
	return 1
}

// CallCost calculates the runtime cost for CEL function calls
func (l *CostEstimator) CallCost(function, overloadId string, args []ref.Val, result ref.Val) *uint64 {
	switch function {
	case "scores":
		// each scores returns a list
		var totalCost uint64 = common.ListCreateBaseCost
		if result != nil {
			if lister, ok := result.(traits.Lister); ok {
				// each item is a map
				size := uint64(lister.Size().(types.Int))
				totalCost += size * (common.SelectAndIdentCost + common.MapCreateBaseCost)
			}
		}
		return &totalCost
	case "parseJSON":
		var totalCost uint64 = common.MapCreateBaseCost
		if len(args) >= 1 {
			// Calculate the traversal cost of input string
			inputSize := actualSize(args[0])
			traversalCost := uint64(math.Ceil(float64(inputSize) * common.StringTraversalCostFactor))
			// Recursively calculate the cost of result structure
			totalCost = traversalCost + calculateStructCost(result)
		}
		return &totalCost
	default:
		return nil
	}
}

// calculateStructCost recursively calculates the cost of data structures
func calculateStructCost(val ref.Val) uint64 {
	if val == nil {
		return 0
	}

	switch v := val.(type) {
	case traits.Mapper:
		return calculateMapCost(v)
	case traits.Lister:
		return calculateListCost(v)
	default:
		return common.ConstCost
	}
}

// calculateMapCost computes cost for map structures
func calculateMapCost(v traits.Mapper) uint64 {
	cost := uint64(common.MapCreateBaseCost)

	it := v.Iterator()
	for it.HasNext() == types.True {
		key := it.Next()
		if value := v.Get(key); value != nil {
			cost += calculateStructCost(value)
		}
	}

	return cost
}

// calculateListCost computes cost for list structures
func calculateListCost(v traits.Lister) uint64 {
	cost := uint64(common.ListCreateBaseCost)

	size := v.Size().(types.Int)
	for i := types.Int(0); i < size; i++ {
		if item := v.Get(types.Int(i)); item != nil {
			cost += calculateStructCost(item)
		}
	}

	return cost
}
