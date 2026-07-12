package marketapp

import (
	"encoding/json"
	"math/rand"
	"os"
	"testing"
	"time"
)

func testApp(t *testing.T) *App {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Restock.RandLow = 1
	cfg.Restock.RandHigh = 1
	return &App{cfg: cfg, configDir: t.TempDir(), rand: rand.New(rand.NewSource(1))}
}

func mustWriteJSON(t *testing.T, path string, value interface{}) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func mustWriteText(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		t.Fatal(err)
	}
}

type clearStockRepository struct {
	counts            map[string]int
	stock             map[string]map[uint32]int
	maxAddInfo        int32
	collectCalls      int
	creatureIDs       []int32
	createCreatureErr error
	creatureCreates   []creatureCreateCall
	creatureCount     int
}

type creatureCreateCall struct {
	dbName  string
	ownerID uint32
	itemID  uint32
}

func (r *clearStockRepository) EnsureMarketTables([]string, time.Time) ([]string, error) {
	return nil, nil
}

func (r *clearStockRepository) LoadCollectRows(string, string, uint32, bool) ([]collectRow, error) {
	return nil, nil
}

func (r *clearStockRepository) LoadSystemCollectRows(string, string, uint32) ([]collectRow, error) {
	r.collectCalls++
	return nil, nil
}

func (r *clearStockRepository) LoadMarketStock(dbName string, _ uint32, _ map[uint32]int) (map[uint32]int, error) {
	out := map[uint32]int{}
	for id, count := range r.stock[dbName] {
		out[id] = count
	}
	return out, nil
}

func (r *clearStockRepository) LoadMaxAddInfo(string, int32) (int32, error) {
	return r.maxAddInfo, nil
}

func (r *clearStockRepository) CreateCreatureItem(dbName string, ownerID uint32, itemID uint32) (int32, error) {
	if r.createCreatureErr != nil {
		return 0, r.createCreatureErr
	}
	r.creatureCreates = append(r.creatureCreates, creatureCreateCall{dbName: dbName, ownerID: ownerID, itemID: itemID})
	if len(r.creatureIDs) == 0 {
		return int32(7000 + len(r.creatureCreates)), nil
	}
	id := r.creatureIDs[0]
	r.creatureIDs = r.creatureIDs[1:]
	return id, nil
}

func (r *clearStockRepository) CountSystemStock(dbName string, _ uint32) (int, error) {
	return r.counts[dbName], nil
}

func (r *clearStockRepository) DeleteSystemStock(dbName string, _ uint32) (int64, error) {
	count := r.counts[dbName]
	r.counts[dbName] = 0
	return int64(count), nil
}

func (r *clearStockRepository) CountSystemCreatureItems(string, uint32) (int, error) {
	return r.creatureCount, nil
}

func (r *clearStockRepository) DeleteSystemCreatureItems(string, uint32) (int64, error) {
	count := r.creatureCount
	r.creatureCount = 0
	return int64(count), nil
}

var _ Repository = (*clearStockRepository)(nil)

func bytePtr(v byte) *byte {
	return &v
}
