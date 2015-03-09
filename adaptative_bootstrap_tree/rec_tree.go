package rectree

import (
	"github.com/alonsovidales/pit/log"
	//"sort"
)

const (
	MAX_ELEMS = 4
)

type BoostrapRecTree interface {
	GetBestRecommendation(recordId uint64, recomendations uint64) ([]uint64)
}

type tNode struct {
	value uint64

	/*bestRecL []*scoresClassifications
	bestRecU []*scoresClassifications
	bestRecD []*scoresClassifications*/
	like     *tNode
	unknown  *tNode
	dislike  *tNode
}

type Tree struct {
	BoostrapRecTree

	tree map[uint64]*tNode
	maxDeep int
	maxScore uint8
	totalRecs int
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
	scoreL float64
	scoreH float64
	scoreU float64
}

type scoresClassifications struct {
	score float64
	avg float64
	elemId uint64
}

/*type ByClassif []*scoresClassifications

func (a ByClassif) Len() int           { return len(a) }
func (a ByClassif) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByClassif) Less(i, j int) bool { return a[i].score > a[j].score }*/

func GetNewTree(records []map[uint64]uint8, maxDeep int, maxScore uint8) (tr *Tree) {
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
		}
	}

	tr = &Tree {
		maxDeep: maxDeep,
		maxScore: maxScore,
		totalRecs: len(records),
		tree: make(map[uint64]*tNode),
	}

	for element, pos := range elemsPos {
		log.Debug("----->>>> Creating tree from:", element)
		delete(elemsPos, element)
		tr.tree[element] = tr.getTreeNode(element, elemsPos, elementsTotals, records, 1)
		elemsPos[element] = pos
	}

	return
}

func (tr *Tree) GetBestRecommendation(recordId uint64, recomendations uint64) (rec []uint64) {
	return
}

func (tr *Tree) getTreeNode(fromElem uint64, elemsPos map[uint64]int, elementsTotals []elemTotals, records []map[uint64]uint8, deep int) (tn *tNode) {
	log.Debug("Records:", len(records))
	tn = &tNode {
		value: fromElem,
	}

	if len(elemsPos) == 0 || deep > tr.maxDeep || len(records) < tr.totalRecs / 10 {
		return
	}

	totals := make([]elemSums, len(elementsTotals))

	likeRecords := []map[uint64]uint8{}
	hateRecords := []map[uint64]uint8{}
	unknownRecords := []map[uint64]uint8{}

	for _, record := range records {
		if v, ok := record[fromElem]; ok {
			if v >= tr.maxScore / 2 {
				likeRecords = append(likeRecords, record)
			} else {
				hateRecords = append(hateRecords, record)
			}

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
		} else {
			unknownRecords = append(unknownRecords, record)
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

	/*classifsL := []*scoresClassifications{}
	classifsD := []*scoresClassifications{}
	classifsU := []*scoresClassifications{}*/
	var scoreL, scoreH, scoreU float64

	for _, pos := range elemsPos {
		if totals[pos].nL > 0 {
			scoreL = float64(totals[pos].sumL*totals[pos].sumL - totals[pos].sum2L) / float64(totals[pos].nL)
			/*if deep > tr.maxDeep && uint8(totals[pos].sumL / totals[pos].nL) > tr.maxScore / 2 {
				classifsL = append(classifsL, &scoresClassifications{
					score: scoreL,
					elemId: elementsTotals[pos].elemId,
					avg: float64(totals[pos].sumL) / float64(totals[pos].nL),
				})
			}*/
		} else {
			scoreL = 0
		}
		if totals[pos].nH > 0 {
			scoreH = float64(totals[pos].sumH*totals[pos].sumH - totals[pos].sum2H) / float64(totals[pos].nH)
			/*if deep > tr.maxDeep && uint8(totals[pos].sumH / totals[pos].nH) > tr.maxScore / 2 {
				classifsD = append(classifsD, &scoresClassifications{
					score: scoreH,
					elemId: elementsTotals[pos].elemId,
					avg: float64(totals[pos].sumH) / float64(totals[pos].nH),
				})
			}*/
		} else {
			scoreH = 0
		}
		if totals[pos].nU > 0 {
			scoreU = float64(totals[pos].sumU*totals[pos].sumU - totals[pos].sum2U) / float64(totals[pos].nU)
			/*if deep > tr.maxDeep && uint8(totals[pos].sumU / totals[pos].nU) > tr.maxScore / 2 {
				classifsU = append(classifsU, &scoresClassifications{
					score: scoreU,
					elemId: elementsTotals[pos].elemId,
					avg: float64(totals[pos].sumU) / float64(totals[pos].nU),
				})
			}*/
		} else {
			scoreU = 0
		}

		if maxScoreL < scoreL {
			maxScoreL = scoreL
			maxLike = elementsTotals[pos].elemId
		}
		if maxScoreH < scoreH {
			maxScoreH = scoreH
			maxHate = elementsTotals[pos].elemId
		}
		if maxScoreU < scoreU {
			maxScoreU = scoreU
			maxUnknown = elementsTotals[pos].elemId
		}
	}

	/*if deep > tr.maxDeep {
		sort.Sort(ByClassif(classifsL))
		sort.Sort(ByClassif(classifsD))
		sort.Sort(ByClassif(classifsU))

		tn.bestRecL = classifsL[:MAX_ELEMS]
		tn.bestRecD = classifsD[:MAX_ELEMS]
		tn.bestRecU = classifsU[:MAX_ELEMS]
	} else {*/
	log.Debug("MaxsL:", maxScoreL, maxLike, "Avg:", float64(totals[elemsPos[maxLike]].sumL) / float64(totals[elemsPos[maxLike]].nL), "Deep:", deep, totals[elemsPos[maxLike]])
	log.Debug("MaxsH:", maxScoreH, maxHate, "Avg:", float64(totals[elemsPos[maxHate]].sumH) / float64(totals[elemsPos[maxHate]].nH), "Deep:", deep, totals[elemsPos[maxLike]])
	log.Debug("MaxsU:", maxScoreU, maxUnknown, "Avg:", float64(totals[elemsPos[maxUnknown]].sumU) / float64(totals[elemsPos[maxUnknown]].nU), "Deep:", deep, totals[elemsPos[maxLike]])

	if totals[elemsPos[maxLike]].nL > 0 && totals[elemsPos[maxLike]].sumL / totals[elemsPos[maxLike]].nL >= uint64(tr.maxScore / 2 + 1) {
		pos = elemsPos[maxLike]
		delete(elemsPos, maxLike)
		tn.like = tr.getTreeNode(maxLike, elemsPos, elementsTotals, likeRecords, deep+1)
		elemsPos[maxLike] = pos
	}

	if totals[elemsPos[maxLike]].nH > 0 && totals[elemsPos[maxHate]].sumH / totals[elemsPos[maxLike]].nH >= uint64(tr.maxScore / 2 + 1) {
		pos = elemsPos[maxHate]
		delete(elemsPos, maxHate)
		tn.dislike = tr.getTreeNode(maxHate, elemsPos, elementsTotals, hateRecords, deep+1)
		elemsPos[maxHate] = pos
	}

	if totals[elemsPos[maxLike]].nU > 0 && totals[elemsPos[maxUnknown]].sumU / totals[elemsPos[maxLike]].nU >= uint64(tr.maxScore / 2 + 1) {
		pos = elemsPos[maxUnknown]
		delete(elemsPos, maxUnknown)
		tn.unknown = tr.getTreeNode(maxUnknown, elemsPos, elementsTotals, unknownRecords, deep+1)
		elemsPos[maxUnknown] = pos
	}
	//}

	return
}

func (tr *Tree) printTree() {
	queue := []*tNode{tr.tree[0]}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		/*recsL := make([][2]float64, len(node.bestRecL))
		for i := 0; i < len(recsL); i++ {
			recsL[i][0] = node.bestRecL[i].score
			recsL[i][1] = node.bestRecL[i].avg
		}
		recsD := make([][2]float64, len(node.bestRecD))
		for i := 0; i < len(recsD); i++ {
			recsD[i][0] = node.bestRecD[i].score
			recsD[i][1] = node.bestRecD[i].avg
		}
		recsU := make([][2]float64, len(node.bestRecU))
		for i := 0; i < len(recsU); i++ {
			recsU[i][0] = node.bestRecU[i].score
			recsU[i][1] = node.bestRecU[i].avg
		}*/
		log.Debug("From:", node.value)
		/*log.Debug("Like:", recsL)
		log.Debug("Disl:", recsD)
		log.Debug("Unkn:", recsU)*/

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
