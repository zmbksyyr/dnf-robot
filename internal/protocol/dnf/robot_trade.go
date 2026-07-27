package dnf

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"strconv"

	sqlpkg "robot/internal/foundation/sql"
)

type ShopVo struct {
	TradeItem  int
	Price      int
	ItemNumber int
}

func loadShopVo(ctx context.Context, db *sql.DB, uid uint32, functionType int) (map[int]ShopVo, error) {
	result := make(map[int]ShopVo)
	if db == nil {
		return result, nil
	}

	rows, err := sqlpkg.SelectContext(ctx, db, "select Trade_item,price,item_number from d_starsky.Robot_stall where function_type=? and state=1 and (UID=? or UID=0) order by UID", functionType, uid)
	if err != nil {
		return result, err
	}

	for _, row := range rows {
		if len(row) < 3 || row[0] == "" || row[1] == "" || row[2] == "" {
			continue
		}
		tradeItem, _ := strconv.Atoi(row[0])
		price, _ := strconv.Atoi(row[1])
		itemNumber, _ := strconv.Atoi(row[2])
		if price > 0 {
			result[tradeItem] = ShopVo{TradeItem: tradeItem, Price: price, ItemNumber: itemNumber}
		}
	}
	return result, nil
}

type tradeQuoteSnapshot struct {
	version      uint64
	uid          uint32
	db           *sql.DB
	transactions [24]Transaction
	active       [24]bool
}

func (r *RobotVo) queueTradeQuoteRefreshUnsafe() {
	r.tradeQuoteVersion++
	r.tradeQuotePending = true
	if r.tradeQuoteLoading {
		return
	}
	r.tradeQuoteLoading = true
	go r.refreshTradeQuote()
}

func (r *RobotVo) invalidateTradeQuoteUnsafe() {
	r.tradeQuoteVersion++
	r.tradeQuotePending = false
}

func (r *RobotVo) tradeQuoteSnapshotUnsafe() (tradeQuoteSnapshot, bool) {
	if !r.tradeQuotePending || r.State != StateRun || r.UID == 0 {
		return tradeQuoteSnapshot{}, false
	}
	r.tradeQuotePending = false
	snapshot := tradeQuoteSnapshot{version: r.tradeQuoteVersion, uid: r.UID, db: r.DB}
	for index, transaction := range r.TransactionArr {
		if transaction == nil {
			continue
		}
		snapshot.transactions[index] = *transaction
		snapshot.active[index] = true
	}
	return snapshot, true
}

func (r *RobotVo) refreshTradeQuote() {
	for {
		r.mu.Lock()
		snapshot, ok := r.tradeQuoteSnapshotUnsafe()
		if !ok {
			r.tradeQuoteLoading = false
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), storeQueryTimeout)
		prices, err := loadShopVo(ctx, snapshot.db, snapshot.uid, 1)
		cancel()

		r.mu.Lock()
		if snapshot.version != r.tradeQuoteVersion {
			if !r.tradeQuotePending {
				r.tradeQuoteLoading = false
				r.mu.Unlock()
				return
			}
			r.mu.Unlock()
			continue
		}
		r.tradeQuoteLoading = false
		if r.State != StateRun || r.UID != snapshot.uid {
			r.mu.Unlock()
			return
		}
		if err != nil {
			fmt.Printf("getShopVo query error: %v\n", err)
			prices = nil
		}
		r.applyTradeQuoteUnsafe(calculateTradeQuote(snapshot, prices))
		r.mu.Unlock()
		return
	}
}

func calculateTradeQuote(snapshot tradeQuoteSnapshot, prices map[int]ShopVo) uint32 {
	var money uint32
	for index, transaction := range snapshot.transactions {
		if !snapshot.active[index] || transaction.ItemNum <= 0 {
			continue
		}
		price, ok := prices[int(transaction.ItemId)]
		if !ok || price.Price <= 0 {
			continue
		}
		money += uint32(transaction.ItemNum) * uint32(price.Price)
	}
	return money
}

func (r *RobotVo) applyTradeQuoteUnsafe(money uint32) {
	var previous [32]byte
	binary.LittleEndian.PutUint32(previous[7:11], r.TradeMoney)
	previous[0] = 4
	pkt, err := buildSendPacket(19, uint16(r.PacketID), previous[:], r.Cipher)
	r.PacketID++
	if err == nil && !r.sendRaw(pkt) {
		r.clearTradeUnsafe()
	}

	var next [32]byte
	binary.LittleEndian.PutUint32(next[7:11], money)
	next[11] = 4
	pkt, err = buildSendPacket(19, uint16(r.PacketID), next[:], r.Cipher)
	r.PacketID++
	if err != nil {
		return
	}
	if r.sendRaw(pkt) {
		r.TradeMoney = money
		return
	}
	r.clearTradeUnsafe()
}

func (r *RobotVo) clearTradeUnsafe() {
	r.TradeMoney = 0
	r.LastTradeState = false
	r.LastTradeID = 0
	for index := range r.TransactionArr {
		r.TransactionArr[index] = nil
	}
}
