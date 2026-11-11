package evaluator

import (
	"fmt"
	"os"

	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
)

func (e *Evaluator) onnxSession(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("%s expects (modelPath[, opts])", call.Callee.String())
	}
	modelPath, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("onnx.session: modelPath must be a string")
	}
	if err := native.RequireOnnx("onnx.session"); err != nil {
		return nil, err
	}
	libPath := os.Getenv("GEBLANG_ONNXRUNTIME")
	intraThreads := 0
	if len(args) == 2 {
		if opts, ok := args[1].(runtime.Dict); ok {
			if v, ok := dictStringField(opts, "libPath"); ok {
				libPath = v
			}
			if v, ok := dictField(opts, "intraOpThreads"); ok {
				if n, ok := native.AsInt64(v); ok {
					intraThreads = int(n)
				}
			}
		}
	}
	if libPath == "" {
		libPath = "libonnxruntime.so"
	}
	sess, err := native.NewONNXSession(libPath, modelPath.Value, intraThreads)
	if err != nil {
		return nil, err
	}
	e.onnxMu.Lock()
	id := e.nextOnnxID
	e.nextOnnxID++
	ensureMap(&e.onnxSessions)[id] = sess
	e.onnxMu.Unlock()
	return &runtime.Instance{Class: e.onnxSessionClass(), Fields: map[string]runtime.Value{"handle": runtime.NewInt64(id)}}, nil
}

func (e *Evaluator) onnxSessionClass() *runtime.Class {
	if e.onnxClass != nil {
		return e.onnxClass
	}
	get := func(this *runtime.Instance) (*native.ONNXSession, error) {
		id, ok := this.Fields["handle"].(runtime.Int)
		if !ok {
			return nil, fmt.Errorf("invalid onnx session handle")
		}
		e.onnxMu.Lock()
		defer e.onnxMu.Unlock()
		s, ok := e.onnxSessions[id.Value.Int64()]
		if !ok {
			return nil, fmt.Errorf("onnx session is closed")
		}
		return s, nil
	}
	cls := &runtime.Class{Name: "Session", Module: "onnx", Fields: []runtime.Field{{Name: "handle"}}, Methods: map[string][]runtime.Function{}}
	cls.Methods["run"] = []runtime.Function{{Name: "run", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		s, err := get(this)
		if err != nil {
			return nil, err
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("Session.run expects (inputs)")
		}
		inputDict, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("Session.run: inputs must be a dict of name -> int64 ndarray")
		}
		inputs := map[string]native.ONNXInput{}
		var iterErr error
		inputDict.ForEachEntry(func(_ string, entry runtime.DictEntry) bool {
			name, ok := entry.Key.(runtime.String)
			if !ok {
				return true
			}
			nd, ok := entry.Value.(*runtime.NDArray)
			if !ok || nd.Dtype != runtime.NDInt64 {
				iterErr = fmt.Errorf("Session.run: input %q must be an int64 ndarray", name.Value)
				return false
			}
			shape := make([]int64, len(nd.Shape))
			for i, d := range nd.Shape {
				shape[i] = int64(d)
			}
			inputs[name.Value] = native.NewONNXInput(nd.I64, shape)
			return true
		})
		if iterErr != nil {
			return nil, iterErr
		}
		out, err := s.Run(inputs, s.OutputNames())
		if err != nil {
			return nil, err
		}
		entries := map[string]runtime.DictEntry{}
		for name, o := range out {
			f64 := make([]float64, len(o.Data()))
			for i, v := range o.Data() {
				f64[i] = float64(v)
			}
			shape := make([]int, len(o.Shape()))
			for i, d := range o.Shape() {
				shape[i] = int(d)
			}
			nd := &runtime.NDArray{F64: f64, Dtype: runtime.NDFloat64, Shape: shape, Strides: runtime.RowMajorStrides(shape)}
			key := runtime.String{Value: name}
			entries[dictKey(key)] = runtime.DictEntry{Key: key, Value: nd}
		}
		return runtime.DictFromEntries(entries, nil), nil
	}}}
	cls.Methods["inputnames"] = []runtime.Function{{Name: "inputNames", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		s, err := get(this)
		if err != nil {
			return nil, err
		}
		return onnxStringList(s.InputNames()), nil
	}}}
	cls.Methods["outputnames"] = []runtime.Function{{Name: "outputNames", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		s, err := get(this)
		if err != nil {
			return nil, err
		}
		return onnxStringList(s.OutputNames()), nil
	}}}
	cls.Methods["close"] = []runtime.Function{{Name: "close", Native: func(this *runtime.Instance, _ []runtime.Value) (runtime.Value, error) {
		id, ok := this.Fields["handle"].(runtime.Int)
		if !ok {
			return runtime.Null{}, nil
		}
		e.onnxMu.Lock()
		defer e.onnxMu.Unlock()
		if s, ok := e.onnxSessions[id.Value.Int64()]; ok {
			s.Close()
			delete(e.onnxSessions, id.Value.Int64())
		}
		return runtime.Null{}, nil
	}}}
	e.onnxClass = cls
	return cls
}

func onnxStringList(names []string) *runtime.List {
	out := make([]runtime.Value, len(names))
	for i, n := range names {
		out[i] = runtime.String{Value: n}
	}
	return &runtime.List{Elements: out}
}
