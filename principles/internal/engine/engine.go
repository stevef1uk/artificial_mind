package engine

type Engine struct {
	principle *DynamicPrinciple
	memory    *Memory
}

func NewEngine(principle *DynamicPrinciple, memory *Memory) *Engine {
	return &Engine{principle: principle, memory: memory}
}

func (e *Engine) Execute(action string, params, context map[string]interface{}, perform func(map[string]interface{}) string) (string, []string) {
	// Check ethical rules
	ok, reasons := e.principle.Check(action, params, context)
	if !ok {
		return "", reasons
	}

	// Check memory cache
	if result, found := e.memory.Get(action, params); found {
		return result, nil
	}

	// Execute action
	result := perform(params)
	e.memory.Store(action, params, result)
	return result, nil
}
