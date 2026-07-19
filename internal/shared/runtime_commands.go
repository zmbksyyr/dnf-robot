package shared

// RuntimeOnlineUser is the protocol-independent login command exchanged
// between scheduler and the DNF runtime.
type RuntimeOnlineUser struct {
	IP             string
	Port           int
	DelayMS        int
	Token          string
	UID            int
	CID            int
	MaxReconnect   int
	ReconnectDelay int
	BirthVillage   int
	BirthArea      int
	BirthX         int
	BirthY         int
	DisjointOpen   bool
	DisjointCost   int
	StoreOpen      bool
	StoreTitle     string
}

type RuntimeMoveCommand struct {
	UID      int
	Village  int
	Area     int
	X        int
	Y        int
	MoveType int
	Speed    int
}

type RuntimeShoutCommand struct {
	UID     int
	Message string
	Type    int
}
