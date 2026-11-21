package ast

// DecoratorReferenceKind reports "type", "function" or "" for a non-literal decorator argument.
func DecoratorReferenceKind(expr Expression) string {
	switch e := expr.(type) {
	case *Identifier:
		return "type"
	case *SelectorExpression:
		if e.Optional || e.Name == nil {
			return ""
		}
		if DecoratorReferenceKind(e.Object) == "type" {
			return "type"
		}
	case *FunctionLiteral:
		return "function"
	}
	return ""
}

// DecoratorHasReference reports a type or function reference, including inside collection literals.
func DecoratorHasReference(expr Expression) bool {
	if DecoratorReferenceKind(expr) != "" {
		return true
	}
	switch e := expr.(type) {
	case *ListLiteral:
		for _, el := range e.Elements {
			if DecoratorHasReference(el) {
				return true
			}
		}
	case *SetLiteral:
		for _, el := range e.Elements {
			if DecoratorHasReference(el) {
				return true
			}
		}
	case *DictLiteral:
		for _, en := range e.Entries {
			if DecoratorHasReference(en.Key) || DecoratorHasReference(en.Value) {
				return true
			}
		}
	}
	return false
}
