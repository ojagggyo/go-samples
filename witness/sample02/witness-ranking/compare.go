package main

func CompareWitnesses(current, previous []Witness) []Witness {
	oldMap := make(map[string]Witness, len(previous))

	for _, w := range previous {
		oldMap[w.Name] = w
	}

	result := make([]Witness, 0, len(current))

	for _, now := range current {
		if old, ok := oldMap[now.Name]; ok {
			now.Miss = now.TotalMissed - old.TotalMissed

			if now.SigningKey != "" &&
				old.SigningKey != "" &&
				now.SigningKey != old.SigningKey {
				now.SigningKeyChange = 1
			}
		}

		result = append(result, now)
	}

	return result
}
