# Market protocol reverse notes

Branch: `market-protocol`

This document records verified facts for the pure-protocol market module. Keep
the module isolated from the existing robot store/actor business until the TCP
API is stable.

## Target services

- Normal auction: `df_auction_r`, DB `taiwan_cain_auction_gold`, port `30803`.
- Cera/gold consignment: `df_point_r`, DB `taiwan_cain_auction_cera`, port `30603`.
- Game gateway: `df_game_r` routes client auction commands to the two services.

Game config observed on VM:

- `/home/neople/game/cfg/cain01.cfg`
- `auction_server_ip=127.0.0.1`
- `auction_server_port=30803`
- `cera_auction_server_ip=127.0.0.1`
- `cera_auction_server_port=30603`

## Design direction

Market should run as a separate scheduler/module with a TCP API first. It should
reuse only narrow game-session primitives from `RobotVo`:

- encrypted send packet construction,
- run-state checks,
- selected market actor sessions.

Avoid adding market behavior to the current robot auto/store loop. Prefer a
small fixed market actor pool over randomly taking active robot actors.

Restock strategy preference:

1. Try light hybrid restock: insert virtual-owner listings into auction DB, then
   trigger a runtime refresh/reload via protocol action.
2. If refresh cannot be triggered without restarting market services, implement
   pure-protocol restock using market actors and real inventory.
3. Avoid restarting `df_auction_r` / `df_point_r` as normal operation.

Recycle/buy strategy preference:

- Use pure protocol search + buy/bid paths, not direct DB delete + mail queue.
- Old direct-DB code is reference material for field semantics and edge cases.

## Old implementation facts to keep

Source reference: `C:\Users\Administrator\Desktop\dnf-market-agent-main`.

- Player/system listings cannot be distinguished by `owner_type`; both may be
  `1`.
- Use owner boundary:
  - `owner_id < 90000001`: player listing.
  - `owner_id >= 90000001`: system/virtual-owner listing.
- Also check `charac_info.m_id` when classifying high owner IDs, so a real
  high-id player is not treated as system inventory.
- `auction_main.add_info`:
  - stackable/material items: stack count,
  - normal equipment: quality/internal value, not count,
  - normal equipment restock should use `add_info=0`,
  - special non-pet items may require globally unique high `add_info`,
  - pets require a `creature_items` row and `add_info=creature_items.ui_id`.
- Normal auction fee/deposit model in old code: 5% fee, 10000 deposit.
- Cera/gold consignment model in old code: 2% fee, no deposit.
- Cera token mail item in old code: `2681762`.
- Do not put too many system listings on one fake owner. Old code used
  `rotate_every=10`; practical upper bound was around 15 listings per owner.

## Client command IDs

From `robot-go/internal/dnf/global.go` and `df_game_r` symbols:

| ID | Name |
| --- | --- |
| 185 | `CMDPACKET_AUCTION_ASK_AVERAGE_PRICE` |
| 186 | `CMDPACKET_AUCTION_REGIST_ITEM` |
| 187 | `CMDPACKET_AUCTION_REGIST_CANCEL` |
| 188 | `CMDPACKET_AUCTION_BIDDING` |
| 189 | `CMDPACKET_AUCTION_SEARCH_BY_ITEMKEY` |
| 190 | `CMDPACKET_AUCTION_SEARCH_BY_NOITEMKEY` |
| 191 | `CMDPACKET_AUCTION_MY_REGISTED_ITEM_INFO` |
| 192 | `CMDPACKET_AUCTION_MY_BIDDING_INFO` |
| 193 | `CMDPACKET_AUCTION_MY_AUCTION_HISTORY` |
| 335 | `CMDPACKET_AUCTION_BUY_ITEM_APIECE` |

## Market routing

Most auction command payloads begin with `pay_type`:

- `pay_type == 1`: `CCeraAuctionServerProxy::SendPacket`, cera/gold
  consignment.
- Other values: `CAuctionServerProxy::SendPacket`, normal auction.

`CMDPACKET_AUCTION_BUY_ITEM_APIECE` is an exception in the observed code path:
it does not read `pay_type` and sends only to normal auction. Cera/gold
consignment buying appears to use the `BIDDING` command with `pay_type=1`.

Relevant `df_game_r` dispatch symbols:

| Function | Address |
| --- | --- |
| `Dispatcher_AuctionAskAveragePrice::dispatch_sig` | `0x08213940` |
| `Dispatcher_AuctionRegistItem::dispatch_sig` | `0x08213e8a` |
| `Dispatcher_AuctionRegistCancel::dispatch_sig` | `0x08214b96` |
| `Dispatcher_AuctionBuyItemApiece::dispatch_sig` | `0x08214e44` |
| `Dispatcher_AuctionBidding::dispatch_sig` | `0x0821522e` |
| `Dispatcher_AuctionSearchByItemKey::dispatch_sig` | `0x082159fc` |
| `Dispatcher_AuctionSearchByNoItmeKey::dispatch_sig` | `0x082161e4` |
| `Dispatcher_AuctionMyRegistedItemInfo::dispatch_sig` | `0x08216966` |
| `Dispatcher_AuctionMyBiddingInfo::dispatch_sig` | `0x08216b9a` |
| `Dispatcher_AuctionMyAuctionHistory::dispatch_sig` | `0x08216d02` |

## Request payload read order

Types are little-endian unless noted.

### Search by item key, command 189

Common prefix:

```text
byte   pay_type
uint32 page_or_offset
byte   search_mode_1
byte   search_mode_2
byte   search_mode_3
uint16 item_key_count
uint32 item_key[item_key_count]
```

Normal auction adds:

```text
int16  category_1
int16  category_2
int16  category_3
byte   filter_1
byte   filter_2
```

Cera/gold consignment stops after the `item_key` array.

### Search without item key, command 190

Common prefix:

```text
byte   pay_type
uint32 page_or_offset
uint16 category_or_sort
byte   filter_1
byte   filter_2
byte   filter_3
byte   filter_4
byte   filter_5
```

Normal auction adds:

```text
int16  category_1
int16  category_2
int16  category_3
byte   filter_6
byte   filter_7
```

Cera/gold consignment stops after the five common bytes.

### My registered items, command 191

```text
byte pay_type
```

No other client fields observed. `df_game_r` adds UID and current `charac_no`.

### Register cancel, command 187

```text
byte pay_type
byte auction_id[8]
```

`df_game_r` adds UID and current `charac_no` before forwarding.

### Buy item apiece, command 335

Observed only for normal auction:

```text
int32  money_or_price
int32  count_or_unit_count
byte   auction_id[8]
byte   unknown_binary[?]
```

`df_game_r` checks current character money, subtracts money immediately, saves
money, then forwards to `CAuctionServerProxy`. The second binary length still
needs confirmation from full struct layout or runtime capture.

### Bidding, command 188

Common first field:

```text
byte pay_type
```

Cera/gold consignment branch:

```text
uint32 cera_amount
byte   auction_id[8]
int32  bid_or_gold_amount
```

The game checks `CUser::GetCera()` and `CUser::GetBuyingGold()` before sending.

Normal auction branch:

```text
int32  money
byte   auction_id[8]
byte   unknown_binary[?]
```

The game checks and subtracts current character money before forwarding.

### Register item, command 186

Common first field:

```text
byte pay_type
```

Cera/gold consignment branch read order:

```text
byte   inventory_space
uint16 inventory_slot
uint32 item_id
int32  count_or_add_info
int32  unit_price
int32  instant_price
```

Observed constraints:

- item_id must be in `0x28d288..0x28d299`,
- amount/count must be `<= 1`,
- price must pass the internal price check.

Normal auction branch read order:

```text
byte   inventory_space
uint16 inventory_slot
uint32 item_id
int32  count_or_add_info
int32  start_price
int32  instant_price
int32  unit_price
int16  roi_category[3]
byte   roi_grade[3]
```

Normal branch additionally checks item lock, item existence, stackability, and
price multiplication. `roi_category` defaults to `30000` for stackable items
when sent as zero. `start_price` must be lower than `instant_price` unless the
instant price is the sentinel `-1`; otherwise `df_game_r` returns command error
`0x98`.

## Server-to-client result packets

`df_game_r` converts auction server responses into normal client packets through
`InterfacePacketBuf`.

| Result | Client packet ID | Observed client body |
| --- | --- | --- |
| `Inter_AuctionResultRegist` | `186` | `byte result`, optional `byte subcode` when result is 0, `byte reason_or_result` |
| `Inter_AuctionResultRegistCancel` | `187` | same shape as register result |
| `Inter_AuctionResultItemList` | `189` | `byte 1`, then auction-server payload excluding leading `charac_no` |
| `Inter_AuctionResultMyRegistedItems` | likely `191` | `byte 1`, then auction-server payload excluding leading `charac_no` |
| `Inter_AuctionResultMyBidding` | likely `192` | `byte 1`, then auction-server payload excluding leading `charac_no` |
| `Inter_AuctionResultBuyItemApiece` | `335` | `byte result`, optional `byte subcode` when result is 0 |

The item-list style responses validate that the first 4 bytes from auction
server match the current `charac_no`, then forward the rest as a blob. This is
useful for the first implementation: collect and persist raw result blobs, then
decode item rows incrementally.

Relevant result dispatch symbols:

| Function | Address |
| --- | --- |
| `Inter_AuctionResultRegist::dispatch_sig` | `0x084d6ad6` |
| `Inter_AuctionResultBidding::dispatch_sig_taiwan` | `0x084d6cda` |
| `Inter_AuctionResultRegistCancel::dispatch_sig` | `0x084d741c` |
| `Inter_AuctionResultItemList::dispatch_sig` | `0x084d75bc` |
| `Inter_AuctionResultMyRegistedItems::dispatch_sig` | `0x084d7758` |
| `Inter_AuctionResultMyBidding::dispatch_sig` | `0x084d78f4` |
| `Inter_AuctionResultBuyItemApiece::dispatch_sig` | `0x084d7c8e` |

## Implementation plan

1. Add packet builders and raw result capture in an isolated market package.
2. Expose a narrow TCP API:
   - `marketStatus`
   - `marketSearch`
   - `marketBuy`
   - `marketCancel`
   - `marketRegister`
   - `marketProbeRefresh`
3. Use dedicated market actor sessions. Do not let the existing robot scheduler
   randomly pick actors.
4. Start with search and raw blob capture. Search is the lowest-risk probe and
   also tests whether DB-inserted virtual listings can be refreshed without
   service restart.
5. Add recycle buy/bid once search rows are decoded enough to identify
   `auction_id`, owner, price, item_id, and count.
6. Add restock last. Prefer protocol-register restock unless
   `marketProbeRefresh` proves a safe DB insert plus protocol refresh path.

## Runtime probes

- Inserted a virtual-owner row into `taiwan_cain_auction_gold.auction_main`:
  `auction_id=1, owner_id=90000001, item_id=3037, add_info=10,
  instant_price=1000, unit_price=100`.
- `marketSearch` before and after clean robot reconnect returned the same empty
  list body:
  `0100000000000000ae6bcc6400000000`.
- Conclusion: plain search does not reload DB-inserted auction rows. Hybrid DB
  restock still needs a separate refresh trigger; until one is found, normal
  restock must use protocol registration.

Observed register failure codes:

- `00 d5 ...` in packet `186` is emitted by the normal register branch after
  `CheckItemLock(GetInvenTypeFromItemSpace(space), slot)`, so it indicates the
  submitted `space/slot` does not map to an unlocked loaded inventory item.
- VM closed-loop update, 2026-06-30:
  - `robotsOnline` must keep login `cid=0` as the select-character slot. The
    local `RobotVo.CID` is now resolved from `d_starsky.Dummylist.CID` so market
    packet results carry the real `charac_no`.
  - Offline DB write to `taiwan_cain_2nd.inventory` at `raw_index=105` with
    `item_id=3037,count=500` registered successfully only with
    `inventory_space=0, inventory_slot=105`.
  - The successful register ACK body was
    `010038d22c5e00000000000000000000`; `marketMyRegistered` then returned the
    listing blob and `auction_main` contained `auction_id=2, owner_id=3,
    item_id=3037, add_info=500, price=9000, instant_price=10000, unit_price=20`.
  - `inventory_space=7` and low logical slots kept returning `0x91`; keep using
    the raw slot returned by `marketPrepareStack.register_slot` for protocol
    registration.
  - `marketSearch` still returned an empty/header-only body for this item even
    from another actor; `marketMyRegistered` is the confirmed read-back path so
    far. Buy-apiece by auction id still needs more reverse work.
- VM buy-back update, 2026-07-01:
  - Direct purchase of a normal auction listing is command `188`
    (`marketBid`) with `money=instant_price`, not command `335`.
  - For `auction_id=2`, buyer `uid=17000001/cid=23` sent `marketBid` with
    `money=10000` and got success ACK body `010038d22c5e0000`.
  - `auction_main` then removed the listing. `taiwan_cain_2nd.postal` received:
    buyer mail `receive_charac_no=23,item_id=3037,add_info=500,type=4`; seller
    mail `receive_charac_no=3,gold=19500,type=5`.
  - Failed `marketBid` attempts still subtract money locally before the auction
    service rejects, so automation should only send validated prices. For an
    instant buy, use exactly the parsed `instant_price` from
    `marketMyRegistered`.
  - Command `335` (`BuyItemApiece`) is not the normal instant-buy path for a new
    registered listing. Auction service `AuctionDictionary::BuyItemApiece`
    checks internal state that does not match a fresh listing with
    `buyer_id=-1`.
- The robot store code's safe mapping remains `raw_index = box_index + 2`.
  `marketPrepareStack` exposes that raw index as `register_slot`; callers should
  use it directly for auction registration.
- Raising `taiwan_cain_2nd.inventory.inventory_capacity` to `120` caused robot
  login confirmation timeouts. Restored capacity `16` and cleared the test slots;
  login returned to normal. Market inventory preparation defaults to capacity
  `16` unless explicitly overridden.
- VM equipment update, 2026-07-01:
  - GMTOOL marks inventory type/position conflicts as abnormal red slots. Its
    inventory ranges are raw indexes: quick `1..8`, equipment `9..56`,
    consumable `57..104`, material `105..152`, quest `153..200`,
    profession `201..248`. The previous equipment test used `box_index=103`
    which maps to raw `105`, so it wrote equipment into the material range and
    could make the role hang during login.
  - `marketPrepareEquip` is now constrained to logical `box_index=7..54`
    (raw `9..56`) and writes a minimal equipment slot: type `1`, item id,
    no strengthen/forge, big-endian equipment grade, little-endian endurance.
    `marketCleanInvalidInventory` was added to clear obvious type/range
    conflicts, following GMTOOL's "delete slot" behavior of zeroing the 61-byte
    slot.
  - After clearing the bad raw `105` slot, seller `uid=17000000/cid=3` logged in
    normally again.
- Equipment registration still returns `0x91` for tested ids `10050` and
  `10016`. `10050` is missing as a standalone row from
  `/home/neople/auction/iteminfo.dat`; `10016` exists there but still fails.
  - `df_auction_r` shows the actual second-stage handler
    `HandlerFor_GP_::onAUCTION_REGIST_ITEM_GP` calls
    `Auction::RegistItem(..., DnfItemInfo, ...)`, reading a `DnfItemInfo` block
    from the game-to-auction packet. Stackable material succeeds without needing
    the equipment-specific parts of that structure; equipment requires this
    second-stage `DnfItemInfo` layout to match what the auction service expects.

## DB-backed restock and memory refresh

Verified on 2026-07-01:

- `df_auction_r` loads rows from `taiwan_cain_auction_gold.auction_main` into
  memory during startup through `InterHandler::initInterEvent()` at
  `0x0807a9f0`, which queues `tagAUCTION_DB_GET_REGISTED_ITEM`.
- The same function can be called at runtime with gdb to refresh auction memory
  without restarting `df_auction_r`.
- Robot TCP command added:

```json
marketReloadAuctionMemory {
  "ensure_monthly_tables": true,
  "timeout_ms": 15000
}
```

- The command defaults to:
  - process: `df_auction_r`
  - function: `0x0807a9f0`
  - auction DB: `taiwan_cain_auction_gold`
- `ensure_monthly_tables` creates the current month history tables if missing:
  - `auction_history_YYYYMM`
  - `auction_history_buyer_YYYYMM`
- This matters because on 2026-07-01 the service would not start until
  `auction_history_202607` and `auction_history_buyer_202607` existed.

Closed-loop DB restock test:

1. Inserted a row directly into `auction_main`:
   - `auction_id=9003`
   - `owner_id=3`
   - `item_id=3039`
   - `add_info=31`
   - `price=10000`
   - `instant_price=10900`
   - `unit_price=351`
2. Called `marketReloadAuctionMemory`.
3. `marketMyRegistered` decoded the listing from auction memory.
4. `marketBid` from buyer `uid=17000001/cid=23` succeeded.
5. DB effects:
   - `auction_main` row removed.
   - buyer postal item mail: `item_id=3039, add_info=31, type=4`.
   - seller postal gold mail: `gold=20573, type=5`.

Operational notes:

- Before the 202607 monthly tables existed, auction startup failed with:
  `Fail to exec(select count(*) from auction_history). process exits.`
- A runtime gdb attach pauses the process briefly and is rejected as the final
  architecture; it remains only a reverse/debug probe.

## Direct auction server protocol

Verified on 2026-07-01:

- `df_game_r` is not required for normal auction listing. A tool can connect
  directly to `df_auction_r:30803` and send the game-to-auction GA packet.
- The direct packet header is the service `PACKET_HEADER`, not just the 6-byte
  TCP framing:
  - byte `0`: category,
  - byte `1`: packet id,
  - bytes `2..5`: little-endian total packet size,
  - bytes `6..9`: zero/sequence header area.
- Handler offsets such as `+0x12`, `+0x16`, and `+0x30` are absolute offsets
  from byte `0` of that 10-byte-header packet.
- Normal auction register:
  - request category `0`, packet id `3`,
  - response category `1`, packet id `4`,
  - register result byte is at response offset `0x1a`; non-zero means accepted,
    offset `0x1b` is the translated failure reason when rejected.
- A minimal material/stackable register packet uses:

```text
uint32 +0x12 charac_no/current cid
uint32 +0x16 owner_id; normal DB rows use charac_no here
char[] +0x1a owner_name, max observed 12 bytes plus nul space
byte   +0x27 owner_type/flag; 0 keeps the 10-listing per-owner cap
int32  +0x28 start_price
int32  +0x2c instant_price
DnfItemInfo +0x30:
  byte   +0x30 reserved/type = 0
  uint32 +0x31 item_id
  byte   +0x35 upgrade/grade packed byte = 0
  int32  +0x36 count/add_info
```

Runtime proof:

1. Sent direct GA register for `owner_id=1`, `owner_name=laoxx`,
   `item_id=3037`, `add_info=17`, `price=9000`, `instant_price=10000`.
2. Received AG register result with success byte `1`.
3. Direct GA `MY_REGISTERED` query returned one row.
4. `taiwan_cain_auction_gold.auction_main` contained:
   `auction_id=9006, owner_id=1, owner_name=laoxx, item_id=3037,
   add_info=17, price=9000, instant_price=10000`.

Current Go TCP APIs:

- `marketDirectRegisterItem`
- `marketDirectMyRegistered`
- `marketDirectBid`

These commands bypass online roles and inventory slots. They currently target
normal auction `df_auction_r`; cera/gold consignment needs the GP/point variant
mapped separately.

Direct purchase/bid verification:

- Normal auction buy uses GA packet id `5`, response AG packet id `5`.
- Minimal request fields:

```text
uint32 +0x12 charac_no/current buyer id for response
uint32 +0x16 buyer_id
char[] +0x1a buyer_name
int32  +0x27 money; use instant_price for buy-now
uint64 +0x2b auction_id
```

- Verified by buying `auction_id=9007` directly:
  - virtual seller `90000002` had listed `3037 x23` for `instant_price=10100`,
  - virtual buyer `90000003` sent direct bid with `money=10100`,
  - `auction_main` row was removed,
  - `taiwan_cain_2nd.postal` received buyer item mail
    `receive_charac_no=90000003,item_id=3037,add_info=23,type=4`,
  - seller gold mail was created for `receive_charac_no=90000002,gold=19595,type=5`.
- Important: this direct path does not go through `df_game_r`, so it does not
  perform the game-side money deduction/check. Use it for market automation with
  virtual/system buyers, or add an explicit money accounting policy before using
  it on real player characters.

## Direct point/gold consignment protocol

Verified on 2026-07-01:

- `df_point_r` uses request category `18` and response category `19`.
- The packet IDs and field offsets match normal auction:
  - register-service id `0`,
  - register item id `3`,
  - bidding id `5`,
  - my registered id `8`.
- Unlike `df_auction_r`, `df_point_r` expects a register-service packet on the
  same TCP connection before point business packets. The Go API sends this
  automatically when `point=true`.
- Gold consignment token range from `iteminfo.dat`:
  - `2675336..2675353`, shown as 100, 200, ..., 9000 denominations.
  - `2675345` is the observed 1000 denomination token.
- Minimal successful register:

```json
marketDirectRegisterItem {
  "point": true,
  "cid": 90000400,
  "owner_id": 90000400,
  "owner_name": "gold101",
  "item_id": 2675345,
  "count_or_add_info": 1,
  "start_price": 1000,
  "instant_price": -1
}
```

- Minimal successful bid:

```json
marketDirectBid {
  "point": true,
  "cid": 90000401,
  "buyer_id": 90000401,
  "buyer_name": "buygold",
  "money": 1200,
  "auction_id": 11
}
```

Runtime proof:

- `marketDirectRegisterItem(point=true)` created `auction_id=11`,
  `owner_id=90000400`, `item_id=2675345`, `add_info=1`, `price=1000`,
  `instant_price=-1` in `taiwan_cain_auction_cera.auction_main`.
- `marketDirectBid(point=true,money=1200)` returned success and updated the row:
  `buyer_id=90000401`, `buyer_name=buygold`, `price=1200`, `instant_price=-1`.
- Point/gold consignment is bid/settlement based. A successful bid does not
  immediately remove `auction_main` or create postal rows like normal auction
  buy-now; settlement is handled by the point auction expiry/timer path.
  185ms and did not break the `df_game_r` connection.
- Do not call this repeatedly without need: `initInterEvent()` also queues
  average-price and ROI average-price DB loads, not only registered item reload.

## Non-invasive refresh search

Additional pass on 2026-07-01 after rejecting attach/hot-call as an
architecture:

- `df_monitor_r` TCP packet IDs are recovered from
  `CPacketDecoder::CPacketDecoder()` with table offset
  `0x0c + 4 * (packet_id + 4)`.
  - world megaphone: `0x546`, handler
    `CPacketTranslater::OnMonitorMegaPhoneMsg`.
  - notify auction mail: `0xc1c`, handler
    `CPacketTranslater::OnNotifyAuctionMail`.
  - reload country code: `0x27fe`, handler
    `CPacketTranslater::onReloadCountryCode`.
  - reload security restrict policy: `0x27ff`, handler
    `CPacketTranslater::onReloadSecurityRestrictPolicy`.
- The monitor reload handlers call `CServerHandler::SendAllTcpGameServer`.
  Monitor only tracks DB, manager, and game-server links in the inspected
  server handler path. No auction/point server registration or forwarding path
  was found, so monitor is not a direct auction memory refresh channel.
- `df_auction_r` normal-auction network handler table is complete for
  packet IDs `0..14`:
  register service, ask average price, ask registered count, register item,
  cancel, bid, search by item key, search without item key, my registered, my
  bidding, my history, open/close private store, check ready, buy apiece.
- `df_point_r` has the same point/cera-auction class of handlers. No network
  handler named or shaped like reload registered items was found.
- `df_auction_r` `TIME_CHECK_CONFIG` only watches config/script mtime and
  trace-mask reload state. It does not call `InterHandler::initInterEvent()` or
  `HandlerFor_DB_::onAUCTION_DB_GET_REGISTED_ITEM`.
- `df_game_r` auction proxy reconnect logic sends only
  `PCK_AUCTION_REGIST_GA/GP` and `PCK_AUCTION_CHECK_AUCTION_READY_GA/GP`.
  The ready packet maps to auction handler packet ID `13`, which only reports
  service readiness.
- `df_game_r` `Inter_AuctionNotifyAuctionService` only toggles
  `CAuctionServerProxy` / `CCeraAuctionServerProxy` running state and broadcasts
  `ENUM_NOTIPACKET_AUCTION_NOTIFY_AUCTION_SERVICE` to clients. It is a service
  open/closed notification, not a reload request.

Current conclusion: no supported packet, monitor command, game-proxy command,
or config/signal reload path has been found that reloads `auction_main` into
`df_auction_r` / `df_point_r` memory. The only confirmed memory reload path is
the internal startup initializer `InterHandler::initInterEvent()`, which is not
acceptable as a non-invasive runtime mechanism when invoked by attach/hot-call.

Same-owner DB insert plus protocol register probe, 2026-07-01:

1. While `df_auction_r` was running, inserted three DB-only rows into
   `taiwan_cain_auction_gold.auction_main`:
   - `auction_id=910001..910003`
   - `owner_id=3`, same seller as market actor `uid=17000000/cid=3`
   - `item_id=3038`, `add_info=21..23`
2. Brought the same seller online and successfully registered a separate stack
   item through the normal auction protocol:
   - `item_id=3037`, `count=400`
   - resulting protocol listing `auction_id=9004`
3. `marketMyRegistered` for seller `cid=3` returned only `auction_id=9004`.
   The DB-only rows `910001..910003` were not present in auction memory.
4. Buyer `uid=17000001/cid=23` searched `item_id=3038`; result stayed
   header-only.
5. Buyer attempted `marketBid` against DB-only `auction_id=910001` with the
   exact instant price. Auction service rejected it with ACK subcode `0x96`.

Conclusion for this specific user-requested route: a same-owner normal protocol
registration does not cause the auction service to rescan or merge other rows
from `auction_main`. It only adds the one protocol-registered listing to memory
and DB.

Accepted non-invasive fallback direction:

- Large DB insert plus packet-triggered memory reload is not currently viable
  unless another real service-supported packet is found.
- For a non-invasive market module, restock should be pure protocol: prepare or
  grant inventory to fixed market actors while offline, log them in, send normal
  auction/point register packets, and recycle through protocol bid/cancel paths.
- DB writes may still be used only for offline actor preparation where the game
  already loads that state on login. Auction-service memory itself should be
  changed through auction packets, not attach, patching, or service restart.

Direct service packet refresh probes, 2026-07-01:

- `df_auction_r` TCP packet header layout:
  - byte 0: category
  - byte 1: packet id
  - bytes 2..5: little-endian packet size
  - the receive parser requires a practical minimum total size of 18 bytes.
- Minimal direct packets to running services produced normal ACKs:
  - normal auction `30803`, GA check-ready: category `0`, packet `13`,
    size `18`, ACK `010d13...01`.
  - normal auction `30803`, GA register-service: category `0`, packet `0`,
    size `22`, ACK `010012...`.
  - point auction `30603`, GP check-ready: category `18`, packet `13`,
    size `18`, ACK `130d13...01`.
  - point auction `30603`, GP register-service: category `18`, packet `0`,
    size `22`, ACK `130012...`.
- Refresh validation:
  1. Inserted DB-only normal auction row `auction_id=920001`, owner `3`,
     item `3038`, count/add-info `33`, instant price `14000`.
  2. Before direct packets, seller `marketMyRegistered` was header-only and
     buyer `marketBid` against `920001` failed with subcode `0x96`.
  3. Sent the four direct service packets above to `df_auction_r` and
     `df_point_r`.
  4. After direct packets, seller `marketMyRegistered` was still header-only and
     buyer `marketBid` still failed with subcode `0x96`.
  5. Deleted test row `920001`.
- Conclusion: direct `register service` / `check ready` packets only establish
  or probe service state. They do not cause `df_auction_r` to rescan
  `auction_main`.

Adjacent non-reload packet probes, 2026-07-01:

- Inserted DB-only row `auction_id=930004`, owner `3`, item `3038`, add-info
  `47`, instant price `18000`.
- Before probing, seller `marketMyRegistered` was header-only and buyer
  `marketBid` failed with `0x96`.
- Sent direct TCP packets to `df_auction_r:30803`:
  - packet `1` ask-average-price: returned a normal response.
  - packet `2` ask-registered-item-count: returned a normal response.
  - packet `11` open-private-store: no response for the zero-payload probe.
  - packet `12` close-private-store: no response for the zero-payload probe.
  - packet `13` check-ready: returned the known ready ACK.
- After these adjacent probes, seller `marketMyRegistered` still did not show
  `930004`, and buyer `marketBid` still failed with `0x96`.
- Conclusion: the near-by count/average/private-store handlers operate on
  existing in-memory state and do not rescan `auction_main`.

UDP service-port probe, 2026-07-01:

- `df_auction_r` and `df_point_r` bind UDP on the same numeric ports as their
  TCP service ports (`30803` / `30603`), but the auction binary has no call site
  to `nsl::UDPSocket::recv()` or `nsl::UDPSocket::pollReadEvent()`.
- Static call sites in `df_auction_r`:
  - `UDPSocket::open()` and `UDPSocket::bind()` are called from
    `Auction::Auction()`.
  - `UDPSocket::send()` is called only from
    `Auction::SendMessageToMonitor()`.
  - no auction business path calls UDP receive.
- Runtime probe:
  1. Inserted DB-only row `auction_id=930003`, owner `3`, item `3038`,
     add-info `46`, instant price `16000`.
  2. Seller `marketMyRegistered` stayed header-only.
  3. Buyer `marketBid` for `930003` failed with subcode `0x96`.
  4. Sent UDP packets to `30803` using the same minimal packet shapes as the
     TCP register-service and check-ready probes.
  5. UDP produced no response, `marketMyRegistered` stayed header-only, and
     `marketBid` still failed with `0x96`.
- Conclusion: the auction UDP socket is used as an outbound monitor notification
  socket, not as a hidden reload/control receiver.

Game-side adjacent ports, 2026-07-01:

- `df_game_r` port `10056` is another configured game channel (`cain05`), not
  an admin/control port.
- `127.0.0.1:20011` is `exchange_server_port` for `cain01`; other channels use
  analogous exchange ports (`20012`, `20021`, `20052`, `20056`).
- Game symbols and strings around exchange, monitor reload, and auction proxy
  were checked. The auction-related game code remains:
  - client auction command dispatch,
  - forwarding to `CAuctionServerProxy` / `CCeraAuctionServerProxy`,
  - auction result ACK dispatch back to users,
  - service open/closed notification to clients.
- No game-side exchange/admin/monitor command was found that asks
  `df_auction_r` or `df_point_r` to reload registered auction rows.

Static packet-path sweep, 2026-07-01:

- `tagAUCTION_DB_GET_REGISTED_ITEM::tagAUCTION_DB_GET_REGISTED_ITEM()` has only
  one real constructor call site in `df_auction_r`: `InterHandler::initInterEvent()`
  at `0x0807a9f0`.
- `HandlerFor_DB_::GetAuctionMainFetchResult()` is only called from
  `HandlerFor_DB_::onAUCTION_DB_GET_REGISTED_ITEM()`.
- `InterHandler::registFuncMap()` registers only:
  - `onINTER_DESTORY_CHARACTER`
  - `onINTER_SERVICE_UNAVAILABLE`
  There is no service-available handler that reruns startup DB loading.
- `App::run()` sets TCP max category to `0x19`, but `App::prepareRun()` installs
  only normal auction network handler category `0` and point auction category
  `18`. DB handlers are installed on `DBDispatcher`, not `TCPDispatcher`, so
  forged TCP packets cannot directly dispatch DB transaction handlers.
- `df_game_r` `CAuctionServerProxy` / `CCeraAuctionServerProxy` only send
  register-service and check-ready maintenance packets. User auction actions are
  forwarded as the known auction business packets. No game-side proxy packet was
  found that asks auction/point service to reload registered rows.
- The auction memory insertion path is also narrow:
  `AuctionDictionary::RegistItem()` pushes expiry dictionaries, character
  dictionaries, and `Search::Insert()`. The only bulk DB source found for that
  path is startup `AUCTION_DB_GET_REGISTED_ITEM`; runtime network packets add
  exactly the one item in the register request.
- Linux service command/signal paths were checked as adjacent non-reload names:
  `processCommandLine`, `checkConfigFile`, `Neof_sendTerminateSignal`,
  `Neof_sendSuspendSignal`, `App::readConfig`, and `App::load_script`. These
  paths can start/stop/suspend the service or reload script/config related
  state, but no call path reaches `InterHandler::initInterEvent()`,
  `tagAUCTION_DB_GET_REGISTED_ITEM`, `onAUCTION_DB_GET_REGISTED_ITEM`, or
  `AuctionDictionary::RegistItem()` for DB-backed registered rows.
- Lifecycle call path detail:
  - `InterHandler::init()` calls `InterHandler::registFuncMap()`,
    `nsl::IHandler::init()`, then `InterHandler::initInterEvent()`.
  - `ServiceFactory::startup()` calls registered handler `init()` methods once.
  - `App::run()` calls `ServiceFactory::startup()` once, sets TCP max category,
    and then loops on stop/service-unavailable handling.
  - No runtime loop branch was found that calls `InterHandler::init()` or
    `initInterEvent()` again.

## Pure-protocol restock implementation

Added TCP command `marketBatchRegisterStacks` as the accepted non-invasive
fallback. It does not refresh auction memory from DB. Instead it supports the
protocol-safe workflow:

1. `robotsLogout` the market actor.
2. `marketBatchRegisterStacks` with `prepare=true, register=false` to write
   offline inventory slots in DB.
3. `robotsOnline` the same actor so game loads that inventory normally.
4. `marketBatchRegisterStacks` with `prepare=false, register=true` to send one
   normal `AuctionRegisterItem` packet per listing.

Command shape:

```json
{
  "uid": 17000000,
  "prepare": false,
  "register": true,
  "timeout_ms": 8000,
  "per_item_delay_ms": 300,
  "continue_on_error": false,
  "items": [
    {
      "box_index": 103,
      "item_id": 3037,
      "count": 77,
      "start_price": 7000,
      "instant_price": 8000,
      "unit_price": 104
    }
  ]
}
```

Validation on VM, 2026-07-01:

- Offline prepared seller `uid=17000000/cid=3`, box `103` -> raw/register slot
  `105`, material item `3037`, count `77`.
- Brought seller online with `robotsOnline`.
- Ran `marketBatchRegisterStacks` with `register=true`; auction ACK succeeded:
  body `010038d22c5e00000000000000000000`.
- `marketMyRegistered` showed `auction_id=9005`, item `3037`, count `77`,
  start price `7000`, instant price `8000`.
- Buyer `uid=17000001/cid=23` bought `9005` through normal `marketBid`; ACK
  succeeded and `auction_main` was empty afterward.
- `marketCancel` still timed out in this environment, so cleanup should prefer
  protocol buyback until cancel packet handling is understood.

## Direct point/gold consignment buy path

Follow-up on 2026-07-01 after confirming gold consignment is paid with cera /
token currency, not gold:

- Direct GP register still uses point service `30603`, request category `18`.
- The gold package item identifies the gold amount. In the tested point
  `iteminfo.dat`, `2675345` is the 1000-wan gold package.
- `df_point_r` `HandlerFor_GP_::onAUCTION_BIDDING_GP` only reads one price
  field from the direct packet at absolute offset `0x27`, plus the auction id at
  `0x2b`. That price is the cera/token bid price.
- The missed field rule was the point listing's buy-now shape:
  `price=-1`, `instant_price=<cera/token price>`.
- Earlier failures used normal-auction style values such as `price=1000` and
  `instant_price=1200`. The point/game route rejected that with public reason
  `0x99`.
- This matches the old `dnf-market-agent-main` row template: `buyer_id=-1`,
  `buyer_name=''`, `price=-1`, `instant_price=<restock price>`.

Validated direct GP buy-now flow:

```json
marketDirectRegisterItem {
  "point": true,
  "cid": 3,
  "owner_id": 3,
  "owner_name": "sysgold",
  "item_id": 2675345,
  "count_or_add_info": 1,
  "start_price": -1,
  "instant_price": 1200
}
```

This created a point auction row:

```text
auction_id=19, owner_id=3, buyer_id=-1, item_id=2675345,
add_info=1, price=-1, instant_price=1200
```

Game-side cera buy also succeeds when the buyer has enough
`taiwan_billing.cash_cera.cera`:

```json
marketCeraBid {
  "uid": 17000001,
  "cera_amount": 1200,
  "gold_amount": 10000000,
  "auction_id": "19",
  "timeout_ms": 15000
}
```

Observed result:

```text
ACK body=010169c818dd0000
auction_main row deleted
auction_history_buyer_202607: pre_buyer_id=-1, buyer_id=23,
pre_price=-1, price=1200
cash_cera for account 17000001 decremented by 1200
```

Direct bypass buy also succeeds against `df_point_r` without an online game
role:

```json
marketDirectBid {
  "point": true,
  "cid": 90000601,
  "buyer_id": 90000601,
  "buyer_name": "buydir",
  "money": 1300,
  "auction_id": 20
}
```

This deletes the `auction_main` row and writes
`auction_history_buyer_202607`. It does not check or deduct
`taiwan_billing.cash_cera`, because it bypasses `df_game_r` and talks directly
to the point auction service. Use this only for virtual/system market actors, or
pair it with separate accounting if a real wallet must be charged.

Game-side cera-bid route, same day:

- `df_game_r` has two client-side buy-like dispatchers:
  - `Dispatcher_AuctionBidding::dispatch_sig` for command `188`.
  - `Dispatcher_AuctionBuyItemApiece::dispatch_sig` for command `335`.
- Command `335` is normal auction only. It constructs
  `PCK_AUCTION_BUY_ITEM_APIECE_GA`, checks/subtracts character gold, and sends
  through `CAuctionServerProxy` to normal auction. It has no `pay_type` and does
  not use `CCeraAuctionServerProxy`.
- Command `188` with `pay_type=1` is the cera/gold-consignment path. Its client
  payload is:

```text
byte   pay_type = 1
uint32 cera_amount
byte[8] auction_id
int32  gold_amount
```

- Game first checks `CUser::GetCera()` against `cera_amount`. This is backed by
  `taiwan_billing.cash_cera.cera`, not `cash_cera_point.cera_point`; with only
  `cash_cera_point` funded, the client ACK fails with subcode `0x90`.
- With the corrected point row (`price=-1`, `instant_price=<cera price>`),
  `marketCeraBid` succeeds and performs the wallet deduction.
- Static flow still shows no separate point buy-now packet. The same `BIDDING`
  handler is used; the row's price fields decide whether the point service treats
  it as a valid one-price gold consignment.

Operational wrappers added in Go:

- `marketDirectRegisterGold`: direct point register with `start_price=-1`,
  `instant_price=cera_price`, default `item_id=2675345`.
- `marketDirectBuyGold`: direct point bid with `money=cera_price`; bypasses
  game-side wallet checks.
- `marketCeraBid`: online game-side purchase; requires the buyer role online and
  funded through `taiwan_billing.cash_cera`.

## Normal auction visibility checkpoint

2026-07-01 follow-up:

- Direct normal auction register still returns success and writes DB rows.
- `AUCTION_MY_REGISTED_ITEM_INFO_GA` can read those rows from
  `df_auction_r` memory, so the personal registered dictionary is populated.
- Direct `AUCTION_SEARCH_BY_ITEMKEY_GA` returns the base empty list packet
  (`AG packet id 7`, size `0x21`) for known DB/memory items such as `7441`,
  `440314`, and `600330012`.
- Retesting with a new owner and `owner_type=0` produced the same result:
  personal registered rows exist, global search remains empty.
- `AuctionDictionary::RegistItem` contains the `Search::Insert` call at
  `0x08051b20`, and direct register feeds `DnfItemInfo` from packet offset
  `0x30`, with item id at `DnfItemInfo+1`.

Current operational rule: keep automatic restock limited to `cera` until normal
auction global search/client visibility is proven. Filling normal auction now
creates rows that are visible to "my registered" but not to normal search.

2026-07-01 local follow-up:

- Added a structured direct normal-auction search probe:

```json
marketDirectSearchItem {
  "cid": 1,
  "owner_id": 1,
  "item_keys": [7441],
  "timeout_ms": 5000
}
```

- Added a raw direct-auction probe for trying packet-layout variants without
  rebuilding:

```json
marketDirectSendRaw {
  "point": false,
  "want_packet_id": 7,
  "payload": "000635000000000000000000000000000000010000000100000000000000001f0100000000000000000000000000060007111d0000",
  "timeout_ms": 5000
}
```

- Both commands are exposed through robot TCP. The independent `market` TCP also
  exposes them and defaults host/port from `market_config.json`.
- The structured probe uses the latest verified direct offsets:
  - packet id `6`, expected result id `7`
  - packet size `0x31 + 4 * item_key_count`
  - `cid` at `0x12`, `owner_id` at `0x16`
  - item-key count at `0x20`
  - item-key array starts at `0x31`
  - default upgrade/refine ranges are `0..31`, `0..7`

Correction from live validation:

- Direct item-key search must not write a rarity max byte at packet offset
  `0x2e`. That byte is not part of the item-key search filter. Writing `6`
  made `FindByItem` find candidates internally but return an empty packet.
- Valid direct item-key search keeps offsets `0x2d/0x2e` zero, with refine
  range at `0x2f/0x30`.
- Response body layout:
  - `0x11..0x14`: total matched rows
  - `0x15..0x16`: current response row count
  - rows start at `0x17`
  - direct search row size is `0x89`
- Validation result: direct normal-auction register creates rows visible through
  global item-key search. Example `item_id=7441` returned packet size `8253`,
  total `77`, page count `60`.
- Operational change: normal auction auto-restock can run through the direct
  protocol path. The remote market config was changed back to
  `markets:["auction","cera"]` after validation.

2026-07-01 direct item blob completion:

- `df_game_r` constructs normal register packets with
  `PCK_AUCTION_REGIST_ITEM_GA::PCK_AUCTION_REGIST_ITEM_GA()`.
- The game-built packet size is `0xc5`, not the earlier minimal `0xb5`.
- `DnfItemInfo` still starts at packet offset `0x30`; `ROI_Category` starts at
  `0x99`.
- Stackable/material-safe direct fields:

```text
packet +0x1a char[13] owner/character name
packet +0x27 byte owner_type / premium flag
packet +0x28 int32 start_price
packet +0x2c int32 instant_price
DnfItemInfo +0x00 / packet +0x30 byte item_type
DnfItemInfo +0x01 / packet +0x31 uint32 item_id
DnfItemInfo +0x05 / packet +0x35 byte item_attr, low 5 bits upgrade
DnfItemInfo +0x06 / packet +0x36 int32 stack count or equipment ref
DnfItemInfo +0x0a / packet +0x3a uint16 endurance
DnfItemInfo +0x0c / packet +0x3c int32 inventory add_info/count mirror
DnfItemInfo +0x10 / packet +0x40 byte amplify/ability type
DnfItemInfo +0x11 / packet +0x41 uint16 amplify/ability value
DnfItemInfo +0x1d / packet +0x4d RandomOption, zero is valid for normal stackables
DnfItemInfo +0x2b / packet +0x5b UpgradeSeparateInfo
packet +0x65 avatar emblem area, zero/init for non-avatar
packet +0x83 avatar expansion area, zero/init for non-avatar
packet +0x99 ROI_Category, zero/default is accepted
```

- Go direct builder now emits the `0xc5` game-shaped packet and mirrors stack
  count into both `DnfItemInfo+0x06` and `DnfItemInfo+0x0c` for virtual
  stackable listings.
- Verified after deployment:
  - direct normal register `3038 x21`, owner `mktc5`, returned
    `auction_id=10217`;
  - robot/client `marketSearch` returned command `189`, `body_len=560`, with
    the `mktc5` row first;
  - direct point/gold register still works with the same builder
    (`auction_id=238` for `goldc5`).

2026-07-01 auction item search repair:

- Client item search depends on the auction/point service-side
  `iteminfo.dat`, not only PVF-derived robot catalogs or the direct register
  item blob.
- The canonical source is PVF `etc/iteminfo.dat`. The runtime services load:
  - `/home/neople/auction/iteminfo.dat`
  - `/home/neople/point/iteminfo.dat`
  - some server layouts use `/home/dxf/auction/iteminfo.dat` and
    `/home/dxf/point/iteminfo.dat`.
- Do not copy the raw PVF script block. Raw `etc/iteminfo.dat` starts with the
  binary PVF script marker and makes `df_auction_r`/`df_point_r` crash during
  startup.
- The robot PVF exporter now writes `pvf_iteminfo.dat` as the decoded runtime
  table, one item per line:

```text
item_id rarity usable_job[11] original_level `name` `name2` category
```

- `marketapp` startup syncs `/root/config/pvf_iteminfo.dat` into existing
  auction/point target paths. Services must be restarted after sync because
  auction/point cache this file at startup.
- `market` also exposes `marketSyncItemInfo` for manual resync. It only copies
  the file; restart `df_auction_r` and `df_point_r` if the sync actually writes
  new content.
- Live validation after restart:
  - `/root/config/pvf_iteminfo.dat`,
    `/home/neople/auction/iteminfo.dat`, and
    `/home/neople/point/iteminfo.dat` matched by `cmp`;
  - `3037` exported as category `13002`;
  - robot `marketSearch item_key 3037` returned `body_len=6584`;
  - robot `marketSearch no_item_key category=13002` returned
    `body_len=5352`;
  - for the no-item-key category query, send only `category=13002`; adding
    `category1/category2/category3=-1` made the server return the empty
    16-byte header-only response;
  - direct auction category search on `30803` returned `5376` bytes.
- If the real client still shows no rows while robot/direct search works, check
  the client PVF. A live loopback capture of `df_game_r -> df_auction_r:30803`
  showed the real client sending direct search packet `packet_id=6` with item
  keys `777480`, `778752`, and `2601846`; those keys did not exist in the
  server PVF-derived `iteminfo.dat`. That means the client-side `Script.pvf`
  was not aligned with the server PVF. Replace or repack the client PVF
  `etc/iteminfo.dat` as well, then fully restart the client.

Follow-up after replacing the client PVF:

- Both client PVF files were replaced and restarted:
  - `DNFClient/Script.pvf`
  - `DNFClient/Script/Script.pvf`
- A local PVF probe confirmed the active client PVF contains
  `etc/iteminfo.dat`, including:

```text
3037 1 1 1 1 1 1 1 1 1 1 1 1 1 `無色小晶塊` `name2_3037` 13002
```

- The original client PVF backup also had the same traditional iteminfo name
  for `3037`, so this was not introduced by the server PVF replacement.
- A real-client capture after replacement still did not show normal auction
  search forwarding. During manual client interaction, game -> auction traffic
  remained `packet_id=11` size `170` and `packet_id=12` size `22`, which match
  the `OPEN_PRIVATE_STORE_GA` / `CLOSE_PRIVATE_STORE_GA` area in
  `df_auction_r`, not `SEARCH_BY_ITEMKEY_GA` (`6`) or
  `SEARCH_BY_NOITEMKEY_GA` (`7`).
- The visible client auction UI can display the search window, but the tested
  interaction did not cause the expected auction search command to be sent.
  Therefore the remaining issue is currently client command/UI behavior or a
  missing client-side auction activation path, not auction DB rows,
  `auction/point/iteminfo.dat`, or direct register item blobs.
- Automated local mouse tests are not fully reliable for this client because
  one exact-name test produced no client -> game application payload. Treat
  hand-click captures as authoritative.
- Next useful captures should start before reconnecting the real client, so the
  encrypted client session can be correlated from login through the auction
  click path.
