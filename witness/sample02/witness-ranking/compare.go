package main

// CompareWitnesses compares the current witness list with a previous snapshot.
//
// Miss values:
//
//	-1 : no previous data exists for this witness
//	 0 : no increase in total_missed
//	>0 : total_missed increased
func CompareWitnesses(current, previous []Witness) []Witness {
	oldMap := make(map[string]Witness, len(previous))

	for _, w := range previous {
		oldMap[w.Name] = w
	}

	result := make([]Witness, 0, len(current))

	for _, now := range current {
		// 比較対象が存在しない場合は -1。
		// ranking.js 側でピンク表示になる。
		now.Miss = -1

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
