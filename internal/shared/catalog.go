package shared

type EquipmentCatalogItem struct {
	ID            int    `json:"id"`
	Name          string `json:"name,omitempty"`
	Name2         string `json:"name2,omitempty"`
	Path          string `json:"path,omitempty"`
	Level         int    `json:"level"`
	ItemType      int    `json:"item_type"`
	Slot          string `json:"slot,omitempty"`
	SetKey        string `json:"set_key,omitempty"`
	Rarity        int    `json:"rarity,omitempty"`
	Price         int    `json:"price,omitempty"`
	Value         int    `json:"value,omitempty"`
	Durability    int    `json:"durability,omitempty"`
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
	Village     int    `json:"village"`
	VillageName string `json:"village_name,omitempty"`
	Area        int    `json:"area"`
	Level       int    `json:"level"`
	XMin        int    `json:"x_min"`
	XMax        int    `json:"x_max"`
	YMin        int    `json:"y_min"`
	YMax        int    `json:"y_max"`
	Use         bool   `json:"use"`
}
