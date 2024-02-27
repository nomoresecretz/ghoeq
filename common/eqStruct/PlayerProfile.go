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
	EquipColor                        []TintStruct
	Inventory                         []int16
	Languages                         []uint8
	// TODO: ItemPropertiesStruct
	SpellBuffs   []SpellBuff
	ContainerInv []int16
	CursorBagInv []int16
	// TODO: ItemPropertiesStruct (Container)
	// TODO: ItemPropertiesStruct (Cursor Container)
	SpellBook                                              []int16
	Unknown2374                                            []byte
	Unknown2886                                            []byte
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
	AirSupply                                              uint8
	Texture                                                uint8
	Height, Width, Length, ViewHeight                      float32
	Boat                                                   string
	Autosplit                                              uint8
	Expansions                                             uint8
	Hunger, Thirst                                         int32
	ZoneID                                                 uint32
	BindPoints                                             []BindPoint // TODO: BindPointStruct
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
	AbilitySlotRefresh                                                                                                                                                  uint32
	GroupMembers                                                                                                                                                        []string
	GroupDat                                                                                                                                                            uint32
	EXPAA                                                                                                                                                               uint32
	Title                                                                                                                                                               uint8
	PerAA                                                                                                                                                               uint8
	Haircolor, Beardcolor, Eye1color, Eye2color, Hairstyle, Beard, Luclinface                                                                                           uint8
	ItemMaterial                                                                                                                                                        []byte
	AAArray                                                                                                                                                             []AA_Array
	ATR_DivineRez, ATR_FreeHot, ATR_TargetDA, ATR_SPTWood, ATR_DireCharm, ATR_StrongRoot, ATR_Masco, ATR_MANABURN, ATR_GatherMana, ATR_PetLOH, ATR_Exodus, ATR_MassFear uint32
	AirRemaining                                                                                                                                                        uint16
	AAPts                                                                                                                                                               uint16
	MGBTimer                                                                                                                                                            uint32
	MBitFlags                                                                                                                                                           []int8
	PopSpellTimer                                                                                                                                                       uint32
	LastSheild, LastModulated                                                                                                                                           uint32
	Unknown004                                                                                                                                                          []byte

	bPointer int
}

type BindPoint struct {
	ZoneId  uint32
	X, Y, Z float32
	Heading float32
}

type SpellBuff struct {
	BuffType     uint8
	Level        uint8
	BardModifier uint8
	Activated    uint8
	SpellID      uint16
	Duration     uint16
	Counters     uint16
}

type TintStruct struct {
	Blue, Green, Red uint8
	Use              uint8
}

func (p *PlayerProfile) EQType() EQType { return EQT_PlayerProfile }
func (p *PlayerProfile) bp() *int       { return &p.bPointer }

func (p *PlayerProfile) Unmarshal(b []byte) error {
	p.bPointer = 0

	if err := EQRead(b, p, &p.Checksum, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Unknown004, 2); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Name, 64); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.LastName, 66); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.UniqueGuildID, 64); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Gender, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.GenderChar, 1); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Race, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Class, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.BodyType, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Level, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.LevelChar, 3); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Exp, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Points, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Mana, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.CurHP, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Status, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.STR, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.STA, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.CHA, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.DEX, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.INT, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.WIS, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Face, 0); err != nil {
		return err
	}

	p.EquipType = make([]uint8, 9)
	for i := range p.EquipType {
		if err := EQRead(b, p, &p.EquipType[i], 0); err != nil {
			return err
		}
	}

	p.EquipColor = make([]TintStruct, 9)
	for i := range p.EquipType {
		if err := EQRead(b, p, &p.EquipColor[i].Blue, 0); err != nil {
			return err
		}
		if err := EQRead(b, p, &p.EquipColor[i].Green, 0); err != nil {
			return err
		}
		if err := EQRead(b, p, &p.EquipColor[i].Red, 0); err != nil {
			return err
		}
		if err := EQRead(b, p, &p.EquipColor[i].Use, 0); err != nil {
			return err
		}
	}

	p.Inventory = make([]int16, 30)
	for i := range p.EquipType {
		if err := EQRead(b, p, &p.Inventory[i], 0); err != nil {
			return err
		}
	}

	p.Languages = make([]uint8, 26)
	for i := range p.Languages {
		if err := EQRead(b, p, &p.Languages[i], 0); err != nil {
			return err
		}
	}

	// TODO: ItemPropertiesStruct

	p.bPointer = 616
	p.SpellBuffs = make([]SpellBuff, 15)
	for i := range p.SpellBuffs {
		if err := EQRead(b, p, &p.SpellBuffs[i].BuffType, 0); err != nil {
			return err
		}
		if err := EQRead(b, p, &p.SpellBuffs[i].Level, 0); err != nil {
			return err
		}
		if err := EQRead(b, p, &p.SpellBuffs[i].BardModifier, 0); err != nil {
			return err
		}
		if err := EQRead(b, p, &p.SpellBuffs[i].Activated, 0); err != nil {
			return err
		}
		if err := EQRead(b, p, &p.SpellBuffs[i].SpellID, 0); err != nil {
			return err
		}
		if err := EQRead(b, p, &p.SpellBuffs[i].Duration, 0); err != nil {
			return err
		}
		if err := EQRead(b, p, &p.SpellBuffs[i].Counters, 0); err != nil {
			return err
		}
	}

	p.ContainerInv = make([]int16, 80)
	for i := range p.EquipType {
		if err := EQRead(b, p, &p.Inventory[i], 0); err != nil {
			return err
		}
	}

	p.CursorBagInv = make([]int16, 10)
	for i := range p.EquipType {
		if err := EQRead(b, p, &p.Inventory[i], 0); err != nil {
			return err
		}
	}

	// TODO: ItemPropertiesStruct
	// TODO: ItemPropertiesStruct

	p.bPointer = 1846
	p.SpellBook = make([]int16, 256)
	for i := range 255 {
		if err := EQRead(b, p, &p.SpellBook[i], 0); err != nil {
			return err
		}
	}

	if err := EQRead(b, p, &p.Unknown2374, 512); err != nil {
		return err
	}

	p.MemSpells = make([]int16, 8)
	for i := range 7 {
		if err := EQRead(b, p, &p.MemSpells[i], 0); err != nil {
			return err
		}
	}

	if err := EQRead(b, p, &p.Unknown2886, 16); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.AvaliableSlots, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.X, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Y, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Z, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Heading, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Position, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Platinum, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Gold, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Silver, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Copper, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.PlatinumBank, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.GoldBank, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.SilverBank, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.CopperBank, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.PlatinumCursor, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.GoldCursor, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.SilverCursor, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.CopperCursor, 0); err != nil {
		return err
	}

	p.bPointer = 2988

	p.Skills = make([]int16, 100)
	for i := range p.Skills {
		if err := EQRead(b, p, &p.Skills[i], 0); err != nil {
			return err
		}
	}

	p.InnateSkills = make([]int16, 25)
	for i := range p.InnateSkills {
		if err := EQRead(b, p, &p.Skills[i], 0); err != nil {
			return err
		}
	}

	if err := EQRead(b, p, &p.AirSupply, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Texture, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Height, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Width, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Length, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.ViewHeight, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Boat, 32); err != nil {
		return err
	}

	p.bPointer = 3348
	if err := EQRead(b, p, &p.Autosplit, 0); err != nil {
		return err
	}

	p.bPointer = 3392
	if err := EQRead(b, p, &p.Expansions, 0); err != nil {
		return err
	}

	p.bPointer = 3416
	if err := EQRead(b, p, &p.Hunger, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Thirst, 0); err != nil {
		return err
	}

	p.bPointer = 3444
	if err := EQRead(b, p, &p.ZoneID, 0); err != nil {
		return err
	}

	p.bPointer = 3784
	z := make([]BindPoint, 5)
	p.BindPoints = z
	for i := range 4 {
		x := p.BindPoints[i]
		if err := EQRead(b, p, &x.ZoneId, 0); err != nil {
			return err
		}
		if err := EQRead(b, p, &x.X, 0); err != nil {
			return err
		}
		if err := EQRead(b, p, &x.Y, 0); err != nil {
			return err
		}
		if err := EQRead(b, p, &x.Z, 0); err != nil {
			return err
		}
		if err := EQRead(b, p, &x.Heading, 0); err != nil {
			return err
		}
	}

	// ItemProp bs

	p.bPointer = 4764
	if err := EQRead(b, p, &p.LoginTime, 0); err != nil {
		return err
	}

	// some more inv bs

	p.bPointer = 4944
	if err := EQRead(b, p, &p.Deity, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.GuildID, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Birthday, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.LastLogin, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.TimePlayedMin, 0); err != nil {
		return err
	}

	p.bPointer = 4962
	if err := EQRead(b, p, &p.Fatigue, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.PVP, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Level2, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.ANON, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.GM, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.GuildRank, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Intoxication, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.EQBackground, 0); err != nil {
		return err
	}

	p.bPointer = 4972
	p.SpellSlotRefresh = make([]uint32, 8)
	for i := range p.SpellSlotRefresh {
		if err := EQRead(b, p, &p.SpellSlotRefresh[i], 0); err != nil {
			return err
		}
	}

	p.bPointer = 5008
	if err := EQRead(b, p, &p.AbilitySlotRefresh, 0); err != nil {
		return err
	}

	p.GroupMembers = make([]string, 6)
	for i := range p.GroupMembers {
		if err := EQRead(b, p, &p.GroupMembers[i], 64); err != nil {
			return err
		}
	}

	if err := EQRead(b, p, &p.GroupDat, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.EXPAA, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Title, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.PerAA, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Haircolor, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Beardcolor, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Eye1color, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Eye2color, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Hairstyle, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Beard, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Luclinface, 0); err != nil {
		return err
	}

	p.ItemMaterial = make([]uint8, 9)
	for i := range p.ItemMaterial {
		if err := EQRead(b, p, &p.ItemMaterial[i], 0); err != nil {
			return err
		}
	}

	p.bPointer = 5612
	p.AAArray = make([]AA_Array, 120)
	for i := range p.AAArray {
		if err := EQRead(b, p, &p.AAArray[i].AA, 0); err != nil {
			return err
		}
		if err := EQRead(b, p, &p.AAArray[i].Value, 0); err != nil {
			return err
		}
	}

	p.bPointer = 5900
	if err := EQRead(b, p, &p.AirRemaining, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.AAPts, 0); err != nil {
		return err
	}

	// inventory stuff.

	return nil
}

type AA_Array struct {
	AA    uint8
	Value uint8
}
