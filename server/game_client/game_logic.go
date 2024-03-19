package game_client

import (
	"fmt"
	"log/slog"

	"github.com/nomoresecretz/ghoeq-common/eqStruct"
	"github.com/nomoresecretz/ghoeq/server/assembler"
	"github.com/nomoresecretz/ghoeq/server/stream"
)

const defaultGamePort = 9000

// process is where the different packet types are sent to logic handlers.
// we try to be light, this server is supposed to only keep relevant packets, and let the client do the real work.
//
//gocyclo:ignore
func (c *GameClient) process(p *stream.StreamPacket) error {
	op := c.decoder.GetOp(p.OpCode)

	switch op {
	case "":
		slog.Debug("game client unknown opcode", "opcode", p.OpCode.String())
	case "OP_PlayEverquestRequest":
		return c.handlePlayEQ(p)
	case "OP_LogServer":
		if p.Stream.Dir == assembler.DirServerToClient {
			return c.handleLogServer(p)
		}
	case "OP_PlayerProfile":
		if p.Stream.Dir == assembler.DirServerToClient {
			return handleOpCode(&eqStruct.PlayerProfile{}, p)
		}
	case "OP_EnterWorld":
		return c.handleEnterWorld(p)
	case "OP_ZoneServerInfo":
		return c.handleZoneServerInfo(p)
	case "OP_ZoneEntry":
		return c.handleZoneEntry(p)
	case "OP_MobUpdate":
		return handleOpCode(&eqStruct.SpawnPositionUpdates{}, p)
	case "OP_ZoneSpawns":
		return handleOpCode(&eqStruct.ZoneSpawns{}, p)
	case "OP_SendLoginInfo":
		return c.handleSendLoginInfo(p)
	case "OP_LoginAccepted":
		return c.handleLoginAccept(p)
	case "OP_ClientUpdate":
		return handleOpCode(&eqStruct.SpawnPositionUpdate{}, p)
	case "OP_ManaUpdate":
		return handleOpCode(&eqStruct.ManaUpdate{}, p)
	case "OP_NewZone":
		return handleOpCode(&eqStruct.NewZone{}, p)
	case "OP_Stamina":
		return handleOpCode(&eqStruct.StaminaUpdate{}, p)
	case "OP_SpawnAppearance":
		return handleOpCode(&eqStruct.SpawnAppearance{}, p)
	case "OP_MoveDoor":
		return handleOpCode(&eqStruct.MoveDoor{}, p)
	case "OP_Action":
		return handleOpCode(&eqStruct.Action{}, p)
	case "OP_BeginCast":
		return handleOpCode(&eqStruct.BeginCast{}, p)
	case "OP_Damage":
		return handleOpCode(&eqStruct.Damage{}, p)
	case "OP_ExpUpdate":
		return handleOpCode(&eqStruct.ExpUpdate{}, p)
	case "OP_Consider":
		return handleOpCode(&eqStruct.Consider{}, p)
	case "OP_TargetMouse":
		return handleOpCode(&eqStruct.Target{}, p)
	case "OP_HPUpdate":
		return handleOpCode(&eqStruct.HPUpdate{}, p)
	case "OP_GroundSpawn":
		return handleOpCode(&eqStruct.Object{}, p)
	case "OP_DeleteSpawn":
		return handleOpCode(&eqStruct.DeleteSpawn{}, p)
	case "OP_MOTD":
		return handleOpCode(&eqStruct.ServerMOTD{}, p)
	case "OP_WearChange":
		return handleOpCode(&eqStruct.WearChange{}, p)
	case "OP_Death":
		return handleOpCode(&eqStruct.Death{}, p)
	case "OP_RaidUpdate":
		return c.handleRaidUpdate(p)
	case "OP_ApproveWorld":
		return handleOpCode(&eqStruct.WorldApprove{}, p)
	case "OP_GuildsList":
		return handleOpCode(&eqStruct.GuildsList{}, p)
	case "OP_GuildUpdate":
		return handleOpCode(&eqStruct.GuildUpdate{}, p)
	case "OP_SendZonepoints":
		return handleOpCode(&eqStruct.ZonePoints{}, p)
	case "OP_LFGCommand":
		return c.handleLFGCommand(p)
	case "OP_Weather":
		return handleOpCode(&eqStruct.Weather{}, p)
	case "OP_TimeOfDay":
		return handleOpCode(&eqStruct.Time{}, p)
	case "OP_SpawnDoor":
		return handleOpCode(&eqStruct.SpawnDoors{}, p)
	case "OP_ChannelMessage":
		return handleOpCode(&eqStruct.ChannelMessage{}, p)
	case "OP_ZoneChange":
		return c.handleZoneChange(p)
	case "OP_ReqNewZone":
	case "OP_SetChatServer":
	case "OP_ChecksumSpell":
	case "OP_ChecksumExe":
	case "OP_ExpansionInfo":
	case "OP_ZoneEntryResend":
	case "OP_SendCharInfo":
	case "OP_DataRate":
	case "OP_Animation":
	case "OP_Illusion": // TODO: Handle this one.
	default:
		slog.Info("unhandled type: ", "type", op, "dir", p.Stream.Dir)
	}

	return nil
}

func (c *GameClient) handlePlayEQ(p *stream.StreamPacket) error {
	ply := &eqStruct.PlayRequest{}
	if err := handleOpCode(ply, p); err != nil {
		return err
	}

	if err := dnsCheck(&ply.IP); err != nil {
		return fmt.Errorf("world server dns lookup failure: %w", err)
	}

	Key := fmt.Sprintf("%s:%d", ply.IP, defaultGamePort)
	fKey, rKey := "dst-"+Key, "src-"+Key

	c.parent.mu.Lock()
	c.parent.predicted[fKey] = NewPredict(c, stream.ST_WORLD, assembler.DirClientToServer)
	c.parent.predicted[rKey] = NewPredict(c, stream.ST_WORLD, assembler.DirServerToClient)
	c.parent.mu.Unlock()

	return nil
}

func (c *GameClient) handleLogServer(p *stream.StreamPacket) error {
	ls := &eqStruct.LogServer{}
	if err := handleOpCode(ls, p); err != nil {
		return err
	}

	if c.clientServer == "" {
		c.clientServer = ls.ShortName
	}

	return nil
}

func (c *GameClient) handleEnterWorld(p *stream.StreamPacket) error {
	ew := &eqStruct.EnterWorld{}
	if err := handleOpCode(ew, p); err != nil {
		return err
	}

	if c.matchChar(ew.Name) {
		return nil
	}

	if c.clientCharacter == "" {
		c.clientCharacter = ew.Name
	}

	if c.matchChar(ew.Name) {
		return nil
	}

	if err := c.parent.RegisterCharacter(c); err != nil {
		return err
	}

	return nil
}

func (c *GameClient) handleZoneServerInfo(p *stream.StreamPacket) error {
	s := &eqStruct.ZoneServerInfo{}
	if err := handleOpCode(s, p); err != nil {
		return err
	}

	slog.Debug("zoning event detected", "client", c.ID)

	if err := dnsCheck(&s.IP); err != nil {
		return fmt.Errorf("zone server dns lookup failure: %w", err)
	}

	Key := fmt.Sprintf("%s:%d", s.IP, s.Port)
	fKey, rKey := "dst-"+Key, "src-"+Key

	c.parent.mu.Lock()
	c.parent.predicted[fKey] = NewPredict(c, stream.ST_ZONE, assembler.DirClientToServer)
	c.parent.predicted[rKey] = NewPredict(c, stream.ST_ZONE, assembler.DirServerToClient)
	c.parent.mu.Unlock()

	return nil
}

func (c *GameClient) handleZoneEntryServer(p *stream.StreamPacket) error {
	s := &eqStruct.ZoneEntryServer{}
	if err := handleOpCode(s, p); err != nil {
		return err
	}

	if c.matchChar(s.Name) {
		return nil
	}

	if err := c.parent.RegisterCharacter(c); err != nil {
		return err
	}

	return nil
}

func (c *GameClient) handleZoneEntryClient(p *stream.StreamPacket) error {
	s := &eqStruct.ZoneEntryClient{}
	if err := handleOpCode(s, p); err != nil {
		return err
	}

	if c.matchChar(s.CharName) {
		return nil
	}

	if err := c.parent.RegisterCharacter(c); err != nil {
		return err
	}

	return nil
}

func (c *GameClient) handleSendLoginInfo(p *stream.StreamPacket) error {
	li := &eqStruct.LoginInfo{}
	if err := handleOpCode(li, p); err != nil {
		return err
	}

	if c.matchAcct(li.Account) {
		return nil
	}

	c.clientSessionID = li.Account
	if err := c.parent.RegisterAcct(c); err != nil {
		return err
	}

	return nil
}

func (c *GameClient) handleLoginAccept(p *stream.StreamPacket) error {
	li := &eqStruct.LoginAccepted{}
	if err := handleOpCode(li, p); err != nil {
		return err
	}

	if c.matchAcct(li.Account) {
		return nil
	}

	c.clientSessionID = li.Account
	if err := c.parent.RegisterAcct(c); err != nil {
		return fmt.Errorf("failed to register acct: %w", err)
	}

	return nil
}

func (c *GameClient) handleRaidUpdate(p *stream.StreamPacket) error {
	switch len(p.Packet.Payload) { // Hacky way to avoid tracking raid state
	case lenRaidCreate:
		return handleOpCode(&eqStruct.RaidCreate{}, p)
	case lenRaidAddMember:
		return handleOpCode(&eqStruct.RaidAddMember{}, p)
	case lenRaidGeneral:
		return handleOpCode(&eqStruct.RaidGeneral{}, p)
	default:
		slog.Info("unhandled type: ", "type", "OP_RaidUpdate", "len", len(p.Packet.Payload), "dir", p.Stream.Dir)
	}

	return nil
}

func (c *GameClient) handleLFGCommand(p *stream.StreamPacket) error {
	switch len(p.Packet.Payload) { // Hacky way to avoid tracking raid state
	case lenLFGAppearance:
		return handleOpCode(&eqStruct.LFGAppearance{}, p)
	case lenLFGCommand:
		return handleOpCode(&eqStruct.LFG{}, p)
	default:
		slog.Info("unhandled type: ", "type", "OP_RaidUpdate", "len", len(p.Packet.Payload), "dir", p.Stream.Dir)
	}

	return nil
}

// this is the goodbye from our old zone.
func (c *GameClient) handleZoneChangeServer(p *stream.StreamPacket) error {
	zc := &eqStruct.ZoneChange{}
	if err := handleOpCode(zc, p); err != nil {
		return err
	}

	switch zc.Success {
	case 0:
		panic("wtf how'd we get here")
	case 1:
	default:
		return nil // some error, we don't care
	}

	p.Stream.Close()
	slog.Info("closing legacy zone stream")

	return nil
}

func (c *GameClient) handleZoneChange(p *stream.StreamPacket) error {
	switch p.Stream.Dir {
	case assembler.DirClientToServer:
		return handleOpCode(&eqStruct.ZoneChangeReq{}, p)
	case assembler.DirServerToClient:
		return c.handleZoneChangeServer(p)
	default:
		slog.Info("unhandled type: ", "type", "OP_ZoneChange", "len", len(p.Packet.Payload), "dir", p.Stream.Dir)
	}

	return nil
}

func (c *GameClient) handleZoneEntry(p *stream.StreamPacket) error {
	switch p.Stream.Dir {
	case assembler.DirServerToClient:
		return c.handleZoneEntryServer(p)
	case assembler.DirClientToServer:
		return c.handleZoneEntryClient(p)
	default:
		slog.Info("unhandled type: ", "type", p, "dir", p.Stream.Dir)
	}

	return nil
}
