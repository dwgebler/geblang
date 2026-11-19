package ast

// BlockContainsYield reports whether block yields outside any nested function, which makes its function a generator.
func BlockContainsYield(block *BlockStatement) bool {
	if block == nil {
		return false
	}
	for _, stmt := range block.Statements {
		if statementContainsYield(stmt) {
			return true
		}
	}
	return false
}

func statementContainsYield(stmt Statement) bool {
	switch s := stmt.(type) {
	case *YieldStatement:
		return true
	case *IfStatement:
		if BlockContainsYield(s.Consequence) || BlockContainsYield(s.Alternative) {
			return true
		}
		for _, elseif := range s.ElseIfs {
			if BlockContainsYield(elseif.Body) {
				return true
			}
		}
	case *WhileStatement:
		return BlockContainsYield(s.Body)
	case *ForStatement:
		return BlockContainsYield(s.Body)
	case *WithStatement:
		return BlockContainsYield(s.Body)
	case *TryStatement:
		if BlockContainsYield(s.Body) || BlockContainsYield(s.Finally) {
			return true
		}
		for _, catch := range s.Catches {
			if BlockContainsYield(catch.Body) {
				return true
			}
		}
	case *MatchStatement:
		for _, c := range s.Cases {
			if BlockContainsYield(c.Body) {
				return true
			}
		}
	case *SelectStatement:
		if BlockContainsYield(s.Default) {
			return true
		}
		for _, c := range s.Cases {
			if BlockContainsYield(c.Body) {
				return true
			}
		}
	}
	return false
}
