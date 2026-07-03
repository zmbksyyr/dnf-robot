package shared

import "time"

// ---- catalog.go ----
type EquipmentCatalogItem struct {
	ID            int    `json:"id"`
	Path          string `json:"path,omitempty"`
	Level         int    `json:"level"`
	ItemType      int    `json:"item_type"`
	Slot          string `json:"slot,omitempty"`
	SetKey        string `json:"set_key,omitempty"`
	Rarity        int    `json:"rarity,omitempty"`
	Price         int    `json:"price,omitempty"`
	Value         int    `json:"value,omitempty"`
	Attach        string `json:"attach,omitempty"`
	Trade         bool   `json:"trade,omitempty"`
	NoTrade       bool   `json:"no_trade,omitempty"`
	TradeBlock    bool   `json:"trade_block,omitempty"`
	CanTrade      *bool  `json:"available_trade,omitempty"`
	CanAuction    *bool  `json:"available_auction,omitempty"`
	CanShop       *bool  `json:"available_shop,omitempty"`
	CanDrop       *bool  `json:"available_drop,omitempty"`
	Auction       bool   `json:"auction,omitempty"`
	Shop          bool   `json:"shop,omitempty"`
	BadName       bool   `json:"bad_name,omitempty"`
	NeedMaterial  bool   `json:"need_material,omitempty"`
	BasicMaterial bool   `json:"basic_material,omitempty"`
	Icon          string `json:"icon,omitempty"`
	FieldImage    string `json:"field_image,omitempty"`
	SubType       int    `json:"sub_type,omitempty"`
	Expire        bool   `json:"expire,omitempty"`
	StackLimit    int    `json:"stack_limit,omitempty"`
	UseJob        []int  `json:"use_job,omitempty"`
}

type MapCatalogItem struct {
	Village int  `json:"village"`
	Area    int  `json:"area"`
	Level   int  `json:"level"`
	XMin    int  `json:"x_min"`
	XMax    int  `json:"x_max"`
	YMin    int  `json:"y_min"`
	YMax    int  `json:"y_max"`
	Use     bool `json:"use"`
}

// ---- robot_state.go ----
type DesiredState string

const (
	DesiredUnknown DesiredState = ""
	DesiredOnline  DesiredState = "online"
	DesiredOffline DesiredState = "offline"
	DesiredMove    DesiredState = "move"
	DesiredShout   DesiredState = "shout"
	DesiredStore   DesiredState = "store"
	DesiredCleanup DesiredState = "cleanup"
)

type ActualState string

const (
	ActualUnknown      ActualState = ""
	ActualStopped      ActualState = "stopped"
	ActualConnecting   ActualState = "connecting"
	ActualLogin        ActualState = "login"
	ActualRunning      ActualState = "running"
	ActualDisconnected ActualState = "disconnected"
	ActualError        ActualState = "error"
)

type Phase string

const (
	PhaseUnknown   Phase = ""
	PhaseAssigned  Phase = "assigned"
	PhaseExecuting Phase = "executing"
	PhaseConfirmed Phase = "confirmed"
	PhaseFailed    Phase = "failed"
)

type RobotState struct {
	UID          int          `json:"uid"`
	CID          int          `json:"cid,omitempty"`
	ActorID      string       `json:"actor_id,omitempty"`
	DesiredState DesiredState `json:"desired_state,omitempty"`
	ActualState  ActualState  `json:"actual_state,omitempty"`
	Phase        Phase        `json:"phase,omitempty"`
	LockVersion  int64        `json:"lock_version"`
	LastError    string       `json:"last_error,omitempty"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

func RuntimeActualState(stateName string, disconnectReason int, missingCore bool) ActualState {
	if missingCore {
		return ActualError
	}
	if disconnectReason != 0 {
		return ActualDisconnected
	}
	switch stateName {
	case "running":
		return ActualRunning
	case "init":
		return ActualConnecting
	case "login":
		return ActualLogin
	case "stop", "clean", "offline":
		return ActualStopped
	case "wrong", "broken":
		return ActualError
	default:
		return ActualUnknown
	}
}

func DesiredFromOperation(operation string, onlineDesired bool) DesiredState {
	switch operation {
	case "cleanup", "deleting":
		return DesiredCleanup
	case "store":
		return DesiredStore
	case "move":
		return DesiredMove
	case "shout":
		return DesiredShout
	}
	if onlineDesired {
		return DesiredOnline
	}
	return DesiredOffline
}
