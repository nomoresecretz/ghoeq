package eqStruct

type PlayerProfile struct {
	Checksum                          uint32
	Name, LastName                    string
	UniqueGuildID                     uint32
	Gender                            uint8
	GenderChar                        string
	Race                              uint16
	Class                             uint16
	BodyType                          uint16
	Level                             uint8
	LevelChar                         string
	Exp                               uint32
	Points                            int16
	Mana                              int16
	CurHP                             int16
	Status                            uint16
	STR, STA, CHA, DEX, INT, AGI, WIS int16
	Face                              uint8
	EquipType                         []uint8
	TintProfile                       []byte
	Inventory                         []uint16
	Languages                         []uint8
	// TODO: ItemPropertiesStruct
	// TODO: SpellBuffStruct
	ContainerInv []int16
	CursorBagInv []int16
	// TODO: ItemPropertiesStruct (Container)
	// TODO: ItemPropertiesStruct (Cursor Container)
	SpellBook                                              []int16
	Unknown2374                                            byte
	MemSpells                                              []int16
	AvaliableSlots                                         uint16
	Y, X, Z, Heading                                       float32
	Position                                               uint32
	Platinum, Gold, Silver, Copper                         int32
	PlatinumBank, GoldBank, SilverBank, CopperBank         int32
	PlatinumCursor, GoldCursor, SilverCursor, CopperCursor int32
	Currency                                               []int32
	Skills                                                 []int16
	InnateSkills                                           []int16
	AirSupply, Texture                                     uint8
	Height, Width, Length, ViewHeight                      float32
	Boat                                                   string
	Autosplit                                              uint8
	Expansions                                             uint8
	Hunger, Thirst                                         int32
	ZoneID                                                 uint32
	BindPoints                                             []byte // TODO: BindPointStruct
	// TODO: Bank ItemStructs
	// TODO: Bank Bag ItemStructs
	LoginTime                                                                                                                                                           uint32
	BankInv                                                                                                                                                             []int16
	BankInvCont                                                                                                                                                         []int16
	Deity                                                                                                                                                               uint16
	GuildID                                                                                                                                                             uint16
	Birthday                                                                                                                                                            uint32
	LastLogin                                                                                                                                                           uint32
	TimePlayedMin                                                                                                                                                       uint32
	Fatigue                                                                                                                                                             int8
	PVP                                                                                                                                                                 uint8
	Level2                                                                                                                                                              uint8
	ANON                                                                                                                                                                uint8
	GM                                                                                                                                                                  uint8
	GuildRank                                                                                                                                                           uint8
	Intoxication                                                                                                                                                        uint8
	EQBackground                                                                                                                                                        uint8
	SpellSlotRefresh                                                                                                                                                    []uint32
	AbilitySlotRefresh                                                                                                                                                  []uint32
	GroupMembers                                                                                                                                                        []string
	GroupDat                                                                                                                                                            uint32
	EXPAA                                                                                                                                                               uint32
	Title                                                                                                                                                               uint8
	PerAA                                                                                                                                                               uint8
	Haircolor, Beardcolor, Eye1color, Eye2color, Hairstyle, Beard, Luclinface                                                                                           uint8
	ItemMaterial                                                                                                                                                        []byte
	AAArray                                                                                                                                                             []byte
	ATR_DivineRez, ATR_FreeHot, ATR_TargetDA, ATR_SPTWood, ATR_DireCharm, ATR_StrongRoot, ATR_Masco, ATR_MANABURN, ATR_GatherMana, ATR_PetLOH, ATR_Exodus, ATR_MassFear uint32
	AirRemaining                                                                                                                                                        uint16
	AAPts                                                                                                                                                               uint16
	MGBTimer                                                                                                                                                            uint32
	MBitFlags                                                                                                                                                           []int8
	PopSpellTimer                                                                                                                                                       uint32
	LastSheild, LastModulated                                                                                                                                           uint32
	bPointer                                                                                                                                                            int
}

func (p *PlayerProfile) EQType() EQType { return EQT_PlayerProfile }
func (p *PlayerProfile) bp() *int       { return &p.bPointer }

func (p *PlayerProfile) Unmarshal(b []byte) error {
	p.bPointer = 0

	if err := EQRead(b, p, &p.Gender, 0); err != nil {
		return err
	}
	return nil
}
