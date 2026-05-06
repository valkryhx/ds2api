package openai

func firstOptionalValue(values []any) any {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}
