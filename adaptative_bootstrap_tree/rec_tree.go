package rectree

import (
	"github.com/alonsovidales/pit/log"
)

const (
	MAX_SCORE = 5
	MIN_SCORE = 0
	MAX_TREE_DEEP = 3
)

type BoostrapRecTree interface {
	GetBestRecommendation(recordId uint64, recomendations uint64) ([]uint64)
}

type tNode struct {
	value uint64

	like    *tNode
	unknown *tNode
	dislike *tNode
}

type Tree struct {
	BoostrapRecTree

	tree *tNode
	maxDeep int
}

type elemTotals struct {
	elemId uint64
	sum float64
	sum2 float64
	n uint64
}

type elemSums struct {
	sumL float64
	sum2L float64
	nL uint64
	sumH float64
	sum2H float64
	nH uint64
	sumU float64
	sum2U float64
	nU uint64
	errL float64
	errH float64
	errU float64
}

func GetNewTree(records []map[uint64]uint8, maxDeep int) (tr *Tree) {
	var maxFreq, maxElem uint64

	elementsTotals := []elemTotals{}
	elemsPos := make(map[uint64]int)

	// Get the most commont element in order to be used as root of the tree
	// Calculates the sum and the square sum of all the elements on the
	// records
	i := 0
	for _, record := range records {
		for k, v := range record {
			vUint64 := float64(v) / MAX_SCORE
			v2Uint64 := vUint64*vUint64
			if p, ok := elemsPos[k]; ok {
				elementsTotals[p].sum += vUint64
				elementsTotals[p].sum2 += v2Uint64
				elementsTotals[p].n++
			} else {
				elementsTotals = append(elementsTotals, elemTotals{
					elemId: k,
					sum: vUint64,
					sum2: v2Uint64,
					n: 1,
				})
				elemsPos[k] = i
				i++
			}

			if maxFreq < elementsTotals[elemsPos[k]].n {
				maxFreq = elementsTotals[elemsPos[k]].n
				maxElem = k
			}
		}
	}

	delete(elemsPos, maxElem)
	tr = &Tree {
		maxDeep: maxDeep,
	}
	tr.tree = tr.getTreeNode(maxElem, elemsPos, elementsTotals, records, 1)

	return
}

func (tr *Tree) GetBestRecommendation(recordId uint64, recomendations uint64) (rec []uint64) {
	return
}

func (tr *Tree) getTreeNode(fromElem uint64, elemsPos map[uint64]int, elementsTotals []elemTotals, records []map[uint64]uint8, deep int) (tn *tNode) {
	tn = &tNode {
		value: fromElem,
	}

	if len(elemsPos) == 0 || deep > MAX_TREE_DEEP {
		return
	}

	totals := make([]elemSums, len(elementsTotals))

	for _, record := range records {
		if v, ok := record[fromElem]; ok {
			// We have a score for this element on this record
			// calculate the totals
			for elem, pos := range elemsPos {
				if j, isJ := record[elem]; isJ {
					jFloat64 := float64(j) / MAX_SCORE
					j2Float64 := jFloat64*jFloat64

					if float64(v) >= (MAX_SCORE / 2) {
						totals[pos].sumL += jFloat64
						totals[pos].sum2L += j2Float64
						totals[pos].nL++
					} else {
						totals[pos].sumH += jFloat64
						totals[pos].sum2H += j2Float64
						totals[pos].nH++
					}
				}
			}
		}
	}

	for _, pos := range elemsPos {
		totals[pos].sumU = elementsTotals[pos].sum - totals[pos].sumL - totals[pos].sumH
		totals[pos].sum2U = elementsTotals[pos].sum2 - totals[pos].sum2L - totals[pos].sum2H
		totals[pos].nU = elementsTotals[pos].n - totals[pos].nL - totals[pos].nH
	}

	// Compute the error for each element
	var maxLike, maxHate, maxUnknown uint64
	var pos int
	maxScoreL := 0.0
	maxScoreH := 0.0
	maxScoreU := 0.0
	for _, pos := range elemsPos {
		errL := totals[pos].sum2L - (totals[pos].sumL*totals[pos].sumL) / float64(totals[pos].nL)
		errH := totals[pos].sum2H - (totals[pos].sumH*totals[pos].sumH) / float64(totals[pos].nH)
		errU := totals[pos].sum2U - (totals[pos].sumU*totals[pos].sumU) / float64(totals[pos].nU)

		if maxScoreL < errL {
			maxScoreL = errL
			maxLike = elementsTotals[pos].elemId
		}
		if maxScoreH < errH {
			maxScoreH = errH
			maxHate = elementsTotals[pos].elemId
		}
		if maxScoreU < errU {
			maxScoreU = errU
			maxUnknown = elementsTotals[pos].elemId
		}
	}

	log.Debug("MaxsL:", maxScoreL, maxLike, "Avg:", totals[elemsPos[maxLike]].sumL / float64(totals[elemsPos[maxLike]].nL), "Deep:", deep, totals[elemsPos[maxLike]])
	log.Debug("MaxsH:", maxScoreH, maxHate, "Avg:", totals[elemsPos[maxHate]].sumH / float64(totals[elemsPos[maxHate]].nH), "Deep:", deep, totals[elemsPos[maxLike]])
	log.Debug("MaxsU:", maxScoreU, maxUnknown, "Avg:", totals[elemsPos[maxUnknown]].sumU / float64(totals[elemsPos[maxUnknown]].nU), "Deep:", deep, totals[elemsPos[maxLike]])

	//return

	if totals[elemsPos[maxLike]].sumL / float64(totals[elemsPos[maxLike]].nL) > 0.5 {
		pos = elemsPos[maxLike]
		delete(elemsPos, maxLike)
		tn.like = tr.getTreeNode(maxLike, elemsPos, elementsTotals, records, deep+1)
		elemsPos[maxLike] = pos
	}

	if totals[elemsPos[maxHate]].sumH / float64(totals[elemsPos[maxLike]].nH) > 0.5 {
		pos = elemsPos[maxHate]
		delete(elemsPos, maxHate)
		tn.dislike = tr.getTreeNode(maxHate, elemsPos, elementsTotals, records, deep+1)
		elemsPos[maxHate] = pos
	}

	if totals[elemsPos[maxUnknown]].sumU / float64(totals[elemsPos[maxLike]].nU) > 0.5 {
		pos = elemsPos[maxUnknown]
		delete(elemsPos, maxUnknown)
		tn.unknown = tr.getTreeNode(maxUnknown, elemsPos, elementsTotals, records, deep+1)
		elemsPos[maxUnknown] = pos
	}

	return
}

func (tr *Tree) printTree() {
	queue := []*tNode{tr.tree}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		log.Debug(node.value)

		if node.like != nil {
			queue = append(queue, node.like)
		}
		if node.unknown != nil {
			queue = append(queue, node.unknown)
		}
		if node.dislike != nil {
			queue = append(queue, node.dislike)
		}
	}
}
