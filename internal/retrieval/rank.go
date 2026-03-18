package retrieval

import "sort"

func ScoreFromCosineDistance(distance float64) float64 {
	return 1 - distance
}

func ApplyStableRanks(results []SemanticQueryResult) []SemanticQueryResult {
	ranked := append([]SemanticQueryResult(nil), results...)

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		if ranked[i].FilePath != ranked[j].FilePath {
			return ranked[i].FilePath < ranked[j].FilePath
		}
		return ranked[i].ChunkID < ranked[j].ChunkID
	})

	for i := range ranked {
		ranked[i].Rank = i + 1
	}

	return ranked
}
