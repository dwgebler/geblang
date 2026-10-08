package bytecode

import (
	"strings"
	"sync"

	"geblang/internal/runtime"
)

// chunkSharedMeta is created once per compiled/decoded chunk and shared
// by every VM bound to a copy of that chunk: the prepared (memoised)
// function metadata, the class index, and the type-assert specs are
// written exactly once (under `once`) and read-only afterwards, so
// concurrent VMs share them without copying or re-deriving.
type chunkSharedMeta struct {
	once                         sync.Once
	preparedFunctions            []FunctionInfo
	classIndex                   map[string]int
	typeAssertSpecs              map[int64]vmTypeSpec
	requiresCallSitePolymorphism bool

	// staticOverlay carries static-member assignments keyed by the
	// declared constant-pool index; the pool itself stays immutable.
	staticMu      sync.RWMutex
	staticOverlay map[int64]runtime.Value

	// vmPools recycles wrapper VMs whose run handed out no vm-capturing
	// closures (escapedRefs == 0), keyed by instruction shape so a pool
	// hit can rewrite the wrapper tail in place without reallocating.
	vmPools sync.Map
}

// wrapperShape identifies a wrapper-derived instruction layout: the
// parent code length and the appended wrapper length.
type wrapperShape struct {
	baseLen    int
	wrapperLen int
}

func (meta *chunkSharedMeta) poolFor(shape wrapperShape) *sync.Pool {
	if p, ok := meta.vmPools.Load(shape); ok {
		return p.(*sync.Pool)
	}
	p, _ := meta.vmPools.LoadOrStore(shape, &sync.Pool{})
	return p.(*sync.Pool)
}

func newChunkSharedMeta() *chunkSharedMeta { return &chunkSharedMeta{} }

func (meta *chunkSharedMeta) prepare(chunk Chunk) {
	meta.once.Do(func() {
		cache := map[string]vmTypeSpec{}
		spec := func(typ string) vmTypeSpec {
			typ = strings.TrimSpace(typ)
			if s, ok := cache[typ]; ok {
				return s
			}
			s := parseVMTypeSpec(typ)
			cache[typ] = s
			return s
		}
		functions := append([]FunctionInfo(nil), chunk.Functions...)
		hasPoly := false
		for i := range functions {
			f := &functions[i]
			f.typeParamSet = functionTypeParameterSetOrNil(*f)
			f.requiresParamValidation = functionRequiresParamValidation(*f)
			f.frameMeta = newFrameMeta(*f, spec)
			if f.Async || f.IsGenerator || len(f.Decorators) > 0 {
				hasPoly = true
			}
			if len(f.ParamTypes) == 0 {
				continue
			}
			f.paramTypeSpecs = make([]vmTypeSpec, len(f.ParamTypes))
			for j, typ := range f.ParamTypes {
				if typ != "" {
					f.paramTypeSpecs[j] = spec(typ)
				}
			}
		}
		if !hasPoly {
			for _, classInfo := range chunk.Classes {
				if len(classInfo.Decorators) > 0 || len(classInfo.MethodDecorators) > 0 {
					hasPoly = true
					break
				}
			}
		}
		classIndex := make(map[string]int, len(chunk.Classes))
		for i, classInfo := range chunk.Classes {
			classIndex[strings.ToLower(classInfo.Name)] = i
		}
		var asserts map[int64]vmTypeSpec
		for _, instruction := range chunk.Instructions {
			if instruction.Op != OpTypeAssert || len(instruction.Operands) != 1 {
				continue
			}
			constIdx := instruction.Operands[0]
			if constIdx < 0 || int(constIdx) >= len(chunk.Constants) {
				continue
			}
			typeStr, ok := chunk.Constants[constIdx].(runtime.String)
			if !ok {
				continue
			}
			if asserts == nil {
				asserts = map[int64]vmTypeSpec{}
			}
			asserts[constIdx] = spec(typeStr.Value)
		}
		meta.preparedFunctions = functions
		meta.classIndex = classIndex
		meta.typeAssertSpecs = asserts
		meta.requiresCallSitePolymorphism = hasPoly
	})
}
