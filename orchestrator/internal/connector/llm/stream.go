package llm

func splitStreamText(text string, chunkSize int) []string {
	if chunkSize <= 0 {
		chunkSize = 32
	}

	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}

	parts := make([]string, 0, (len(runes)+chunkSize-1)/chunkSize)
	for start := 0; start < len(runes); start += chunkSize {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		parts = append(parts, string(runes[start:end]))
	}
	return parts
}
