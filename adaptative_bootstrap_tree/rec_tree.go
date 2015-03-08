package rectree

import (
	"github.com/alonsovidales/pit/log"
)

const (
	MAX_CLUSTERS = 10
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

type cluster struct {
	tree *Tree
}

type Tree struct {
	BoostrapRecTree

	tree *tNode
	maxDeep int
	maxScore uint8
}

type elemTotals struct {
	elemId uint64
	sum uint64
	sum2 uint64
	err uint64
	n uint64
}

type elemSums struct {
	sumL uint64
	sum2L uint64
	nL uint64
	sumH uint64
	sum2H uint64
	nH uint64
	sumU uint64
	sum2U uint64
	nU uint64
	errL float64
	errH float64
	errU float64
}

func GetNewTree(records []map[uint64]uint8, maxDeep int, maxScore uint8) (tr *Tree) {
	var maxFreq, maxElem uint64

	elementsTotals := []elemTotals{}
	elemsPos := make(map[uint64]int)

	// Get the most commont element in order to be used as root of the tree
	// Calculates the sum and the square sum of all the elements on the
	// records
	i := 0
	for _, record := range records {
		for k, v := range record {
			vUint64 := uint64(v)
			v2Uint64 := vUint64*vUint64
			if p, ok := elemsPos[k]; ok {
				elementsTotals[p].sum += vUint64
				elementsTotals[p].sum2 += v2Uint64
				elementsTotals[p].err += uint64((maxScore - v) * (maxScore - v))
				elementsTotals[p].n++
			} else {
				elementsTotals = append(elementsTotals, elemTotals{
					elemId: k,
					sum: vUint64,
					sum2: v2Uint64,
					err: uint64((maxScore - v) * (maxScore - v)),
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
		maxScore: maxScore,
	}
	tr.tree = tr.getTreeNode(maxElem, elemsPos, elementsTotals, records, 1)

	return
}

func (tr *Tree) GetBestRecommendation(recordId uint64, recomendations uint64) (rec []uint64) {
	return
}

// defineClusters Use "apriori" in order to define clusters of users based on the
// scored items:
//  - http://en.wikipedia.org/wiki/Apriori_algorithm
func (tr *Tree) defineClusters(records []map[uint64]uint8) (clusters []*cluster) {
	clusters = make([]*cluster, MAX_CLUSTERS)
	elemtsFreq := make(map[uint64]uint64)
	totalElems := 0
	for _, record := range records {
		for k, _ := range record {
			totalElems++
			if _, ok := elementsSet[k]; ok {
				elementsSet[k]++
			} else {
				elementsSet[k] = 1
			}
		}
	}

	// Get the MAX_CLUSTERS with a higest freq
	maxFreqElems := make(map[[]uint64]int)
	minFreq := 0
	for 

	return
}

func (tr *Tree) getTreeNode(fromElem uint64, elemsPos map[uint64]int, elementsTotals []elemTotals, records []map[uint64]uint8, deep int) (tn *tNode) {
	tn = &tNode {
		value: fromElem,
	}

	if len(elemsPos) == 0 || deep > tr.maxDeep {
		return
	}

	totals := make([]elemSums, len(elementsTotals))

	for _, record := range records {
		if v, ok := record[fromElem]; ok {
			// We have a score for this element on this record
			// calculate the totals
			for elem, pos := range elemsPos {
				if j, isJ := record[elem]; isJ {
					jUint64 := uint64(j)
					j2Uint64 := jUint64*jUint64

					if v >= tr.maxScore / 2 {
						totals[pos].sumL += jUint64
						totals[pos].sum2L += j2Uint64
						totals[pos].nL++
					} else {
						totals[pos].sumH += jUint64
						totals[pos].sum2H += j2Uint64
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
		errL := float64(totals[pos].sumL*totals[pos].sumL - totals[pos].sum2L) / float64(totals[pos].nL)
		errH := float64(totals[pos].sumH*totals[pos].sumH - totals[pos].sum2H) / float64(totals[pos].nH)
		errU := float64(totals[pos].sumU*totals[pos].sumU - totals[pos].sum2U) / float64(totals[pos].nU)

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

	log.Debug("MaxsL:", maxScoreL, maxLike, "Avg:", float64(totals[elemsPos[maxLike]].sumL) / float64(totals[elemsPos[maxLike]].nL), "Deep:", deep, totals[elemsPos[maxLike]])
	log.Debug("MaxsH:", maxScoreH, maxHate, "Avg:", float64(totals[elemsPos[maxHate]].sumH) / float64(totals[elemsPos[maxHate]].nH), "Deep:", deep, totals[elemsPos[maxLike]])
	log.Debug("MaxsU:", maxScoreU, maxUnknown, "Avg:", float64(totals[elemsPos[maxUnknown]].sumU) / float64(totals[elemsPos[maxUnknown]].nU), "Deep:", deep, totals[elemsPos[maxLike]])

	if totals[elemsPos[maxLike]].sumL / totals[elemsPos[maxLike]].nL >= uint64(tr.maxScore / 2 + 1) {
		pos = elemsPos[maxLike]
		delete(elemsPos, maxLike)
		tn.like = tr.getTreeNode(maxLike, elemsPos, elementsTotals, records, deep+1)
		elemsPos[maxLike] = pos
	}

	if totals[elemsPos[maxHate]].sumH / totals[elemsPos[maxLike]].nH >= uint64(tr.maxScore / 2 + 1) {
		pos = elemsPos[maxHate]
		delete(elemsPos, maxHate)
		tn.dislike = tr.getTreeNode(maxHate, elemsPos, elementsTotals, records, deep+1)
		elemsPos[maxHate] = pos
	}

	if totals[elemsPos[maxUnknown]].sumU / totals[elemsPos[maxLike]].nU >= uint64(tr.maxScore / 2 + 1) {
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
