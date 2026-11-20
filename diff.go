package main

const maxLCSCells = 4_000_000

// changed returns the indices of lines in cur that are not part of the
// longest common subsequence with old.
func changed(old, cur []string) map[int]bool {
	out := map[int]bool{}
	start := 0
	for start < len(old) && start < len(cur) && old[start] == cur[start] {
		start++
	}
	endOld, endCur := len(old), len(cur)
	for endOld > start && endCur > start && old[endOld-1] == cur[endCur-1] {
		endOld--
		endCur--
	}
	a, b := old[start:endOld], cur[start:endCur]
	if len(a)*len(b) > maxLCSCells {
		for j := range b {
			out[start+j] = true
		}
		return out
	}
	lcs := make([][]int32, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for j < len(b) {
		switch {
		case i < len(a) && a[i] == b[j]:
			i++
			j++
		case i < len(a) && lcs[i+1][j] >= lcs[i][j+1]:
			i++
		default:
			out[start+j] = true
			j++
		}
	}
	return out
}
