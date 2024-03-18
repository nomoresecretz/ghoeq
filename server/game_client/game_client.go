package game_client

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nomoresecretz/ghoeq-common/eqStruct"
	"github.com/nomoresecretz/ghoeq/server/assembler"
	"github.com/nomoresecretz/ghoeq/server/common"
	"github.com/nomoresecretz/ghoeq/server/stream"
)

const (
	lenRaidCreate    = 72
	lenRaidAddMember = 144
	lenRaidGeneral   = 136
	lenLFGAppearance = 8
	lenLFGCommand    = 68
)

const maxPredictTime = 60 * time.Second

var letterTest = regexp.MustCompile(`[a-zA-Z]`)

type predictEntry struct {
	created time.Time
	client  *GameClient
	sType   stream.StreamType
	dir     assembler.FlowDirection
}

type streamPredict map[string]predictEntry

type GameClientWatch struct {
	mu        sync.RWMutex
	clients   map[uuid.UUID]*GameClient
	predicted streamPredict
	ping      chan struct{}
	Decoder   common.OpDecoder
	charMap   map[string]*GameClient
	acctMap   map[string]*GameClient
	db        *DB
}

type GameClient struct {
	ID              uuid.UUID
	Mu              sync.RWMutex
	Streams         map[assembler.Key]*stream.Stream
	Ping            chan struct{}
	decoder         common.OpDecoder
	parent          *GameClientWatch
	clientServer    string // game server name.
	clientSessionID string // Assigned by server.
	clientCharacter string // game client character.
	PlayerProfile   *eqStruct.PlayerProfile
	db              *DB
}

func NewPredict(gc *GameClient, s stream.StreamType, dir assembler.FlowDirection) predictEntry {
	return predictEntry{
		created: time.Now(), // TODO: varify this for testing.
		client:  gc,
		sType:   s,
		dir:     dir,
	}
}

func NewClientWatch() (*GameClientWatch, error) {
	db, err := newDB()
	if err != nil {
		return nil, fmt.Errorf("failed to create db: %w", err)
	}

	return &GameClientWatch{
		clients:   make(map[uuid.UUID]*GameClient),
		ping:      make(chan struct{}),
		predicted: make(streamPredict),
		charMap:   make(map[string]*GameClient),
		acctMap:   make(map[string]*GameClient),
		db:        db,
	}, nil
}

func (c *GameClientWatch) newClient() *GameClient {
	gc := &GameClient{
		ID:      uuid.New(),
		Streams: make(map[assembler.Key]*stream.Stream),
		Ping:    make(chan struct{}),
		decoder: c.Decoder,
		parent:  c,
		db:      c.db,
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.clients[gc.ID] = gc
	c.LockedPing()

	slog.Debug("new game client tracked", "client", gc.ID)

	return gc
}

func (c *GameClient) DeleteStream(s assembler.Key) {
	c.Mu.Lock()
	delete(c.Streams, s)
	c.Mu.Unlock()
}

func (c *GameClient) AddStream(s *stream.Stream) {
	slog.Debug("gameClient stream added")
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.Streams[s.Key] = s
	c.LockedPing()
}

func (c *GameClient) LockedPing() {
	ping := c.Ping
	c.Ping = make(chan struct{})

	close(ping)
}

func (c *GameClientWatch) LockedPing() {
	ping := c.ping
	c.ping = make(chan struct{})

	close(ping)
}

func (c *GameClientWatch) CheckFull(p *stream.StreamPacket, pr predictEntry, b bool) bool {
	invalid := time.Now().Add(maxPredictTime)
	if !b || pr.created.After(invalid) {
		return false
	}

	p.Stream.Dir = pr.dir
	p.Stream.Type = pr.sType
	p.Stream.GameClient = pr.client

	if pr.client == nil {
		return true
	}

	pr.client.AddStream(p.Stream)

	return true
}

func (c *GameClientWatch) CheckReverse(p *stream.StreamPacket, rpr predictEntry, b bool) bool {
	invalid := time.Now().Add(maxPredictTime)
	if !b || rpr.created.After(invalid) {
		return false
	}

	p.Stream.Dir = rpr.dir.Reverse()
	p.Stream.Type = rpr.sType
	p.Stream.GameClient = rpr.client

	if rpr.client == nil {
		return true
	}

	rpr.client.AddStream(p.Stream)

	return true
}

func (c *GameClientWatch) CheckHalf(p *stream.StreamPacket) bool {
	testKey := fmt.Sprintf("dst-%s:%s", p.Stream.Net.Dst().String(), p.Stream.Port.Dst().String())
	testKeyR := fmt.Sprintf("src-%s:%s", p.Stream.Net.Src().String(), p.Stream.Port.Src().String())

	c.mu.RLock()
	prF, okF := c.predicted[testKey]
	prB, okB := c.predicted[testKeyR]
	c.mu.RUnlock()

	if okF {
		p.Stream.Dir = prF.dir
		p.Stream.Type = prF.sType
		p.Stream.GameClient = prF.client

		if prF.client == nil {
			return true
		}

		prF.client.AddStream(p.Stream)

		return true
	}

	if okB {
		p.Stream.Dir = prB.dir
		p.Stream.Type = prB.sType
		p.Stream.GameClient = prB.client

		if prB.client == nil {
			return true
		}

		prB.client.AddStream(p.Stream)

		return true
	}

	return false
}

func (c *GameClientWatch) Check(p *stream.StreamPacket) bool {
	c.mu.RLock()
	pr, ok1 := c.predicted[p.Stream.Key.String()]
	rv := p.Stream.Key.Reverse()
	rpr, ok2 := c.predicted[rv.String()]
	c.mu.RUnlock()

	if r := c.CheckFull(p, pr, ok1); r {
		return r
	}

	if r := c.CheckReverse(p, rpr, ok2); r {
		return r
	}

	if r := c.CheckHalf(p); r {
		return r
	}

	return false
}

func (c *GameClientWatch) newPredictClient(p *stream.StreamPacket) {
	rv := p.Stream.Key.Reverse()
	cli := c.newClient()
	slog.Info("unknown/new client thread seen, adding client", "client", cli.ID, "streamtype", p.Stream.Type.String())
	p.Stream.GameClient = cli
	cli.AddStream(p.Stream)
	c.predicted[rv.String()] = NewPredict(cli, p.Stream.Type, p.Stream.Dir.Reverse())
}

func (gw *GameClientWatch) GracefulStop() {
	for _, gc := range gw.clients {
		gc.Close()
	}

	gw.mu.Lock()
	defer gw.mu.Unlock()

	gw.ClosePing()
}

func (c *GameClientWatch) charMatch(n string) *GameClient {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, gc := range c.clients {
		if gc.clientCharacter == n {
			return gc
		}
	}

	return nil
}

func (c *GameClient) Close() {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.ClosePing()
	c.parent.mu.Lock()
	delete(c.parent.charMap, c.clientCharacter)
	c.parent.mu.Unlock()
}

func (c *GameClientWatch) ClosePing() {
	ping := c.ping
	c.ping = nil
	if ping != nil {
		close(ping)
	}
}

func (c *GameClient) ClosePing() {
	ping := c.Ping
	c.Ping = nil
	if ping != nil {
		close(ping)
	}
}

func (c *GameClient) Run(p *stream.StreamPacket) error {
	if err := c.process(p); err != nil {
		return err
	}

	if err := c.db.Update(p); err != nil {
		return err
	}

	return c.clientSend(p)
}

func (c *GameClient) clientSend(p *stream.StreamPacket) error {
	// TODO: this.

	return nil
}

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
		switch p.Stream.Dir {
		case assembler.DirServerToClient:
			return c.handleZoneEntryServer(p)
		case assembler.DirClientToServer:
			return c.handleZoneEntryClient(p)
		default:
			slog.Info("unhandled type: ", "type", op, "dir", p.Stream.Dir)
		}
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

func (gw *GameClientWatch) matchChar(n string) *GameClient {
	gw.mu.RLock()
	defer gw.mu.RUnlock()

	// TODO: be server aware.
	if c, ok := gw.charMap[n]; ok {
		return c
	}

	return nil
}

func (gw *GameClientWatch) matchAcct(n string) *GameClient {
	gw.mu.RLock()
	defer gw.mu.RUnlock()

	// TODO: be server aware.
	if c, ok := gw.acctMap[n]; ok {
		return c
	}

	return nil
}

func (gw *GameClientWatch) RegisterCharacter(c *GameClient) error {
	gw.mu.Lock()
	defer gw.mu.Unlock()

	cc, ok := gw.charMap[c.clientCharacter]
	if ok {
		if cc != c {
			return fmt.Errorf("character already registered to a different client")
		}
	}

	gw.charMap[c.clientCharacter] = c

	return nil
}

func (gw *GameClientWatch) RegisterAcct(c *GameClient) error {
	gw.mu.Lock()
	defer gw.mu.Unlock()

	cc, ok := gw.acctMap[c.clientSessionID]
	if ok {
		if cc != c {
			return fmt.Errorf("account already registered to a different client")
		}
	}

	gw.acctMap[c.clientSessionID] = c
	return nil
}

func (c *GameClient) matchChar(n string) bool {
	gc := c.parent.matchChar(n)
	if gc == nil || gc == c {
		return false
	}
	slog.Info("matched extra client by charName, reducing", "name", n)

	c.reduce(gc)

	return true
}

func (c *GameClient) matchAcct(n string) bool {
	gc := c.parent.matchAcct(n)
	if gc == nil || gc == c {
		return false
	}
	slog.Info("matched extra client by account, reducing", "name", n)

	c.reduce(gc)
	return true
}

func (c *GameClient) reduce(gc *GameClient) {
	gc.Mu.Lock()
	c.Mu.Lock()

	for _, s := range c.Streams {
		s.GameClient = gc
		gc.Streams[s.Key] = s
		delete(c.Streams, s.Key)
	}

	c.parent.mu.Lock()
	delete(c.parent.clients, c.ID)
	gc.LockedPing()
	c.parent.mu.Unlock()
	c.Mu.Unlock()
	gc.Mu.Unlock()
}

func (c *GameClientWatch) Run(p *stream.StreamPacket) error {
	if c.Check(p) {
		slog.Debug("success matching unknown flow", "stream", p.Stream.Key.String())

		if err := p.Stream.GameClient.Run(p); err != nil {
			return err
		}
		return nil
	}

	c.Check(p)

	if p.Stream.Type == stream.ST_UNKNOWN {
		p.Stream.Identify(p.Packet)
	}

	if c.Check(p) {
		slog.Debug("fallback matching unknown flow", "stream", p.Stream.Key.String())

		if err := p.Stream.GameClient.Run(p); err != nil {
			return err
		}
	}

	switch p.Stream.Type {
	case stream.ST_WORLD, stream.ST_LOGIN: // Wait for a zone event at minimum.
		c.newPredictClient(p)
		return p.Stream.GameClient.Run(p)
	}

	return nil
}

func (c *GameClientWatch) getFirstClient() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for k := range c.clients {
		return k.String()
	}

	return ""
}

// WaitForClient obtains a named client, or can block waiting for first avaliable.
func (c *GameClientWatch) WaitForClient(ctx context.Context, id string) (*GameClient, error) {
	if id == "" {
		c.mu.RLock()
		p := c.ping
		l := len(c.clients)
		c.mu.RUnlock()

		switch l {
		case 0:
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-p:
			}

			id = c.getFirstClient()
		case 1:
			id = c.getFirstClient()
		default:
			return nil, fmt.Errorf("more than 1 game client to pick from, please select one to follow")
		}
	}

	uId, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid id format: %w", err)
	}

	c.mu.RLock()
	cli, ok := c.clients[uId]
	c.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown game client %s", id)
	}

	return cli, nil
}

func (c *GameClient) PingChan() chan struct{} {
	c.Mu.RLock()
	p := c.Ping
	c.Mu.RUnlock()
	return p
}

func (c *GameClient) handlePlayEQ(p *stream.StreamPacket) error {
	ply := &eqStruct.PlayRequest{}
	if err := handleOpCode(ply, p); err != nil {
		return err
	}

	if err := dnsCheck(&ply.IP); err != nil {
		return fmt.Errorf("zone server dns lookup failure: %w", err)
	}

	Key := fmt.Sprintf("%s:%d", ply.IP, 9000)
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
	switch p.Stream.Dir { // Hacky way to avoid tracking raid state
	case assembler.DirClientToServer:
		return handleOpCode(&eqStruct.ZoneChangeReq{}, p)
	case assembler.DirServerToClient:
		return c.handleZoneChangeServer(p)
	default:
		slog.Info("unhandled type: ", "type", "OP_RaidUpdate", "len", len(p.Packet.Payload), "dir", p.Stream.Dir)
	}

	return nil
}

func handleOpCode[T eqStruct.EQStruct](s T, p *stream.StreamPacket) error {
	if _, err := s.Unmarshal(p.Packet.Payload); err != nil {
		slog.Error("failed unmarshaling", "error", err, "struct", s)
		return fmt.Errorf("failed to unmarshal %T: %w", s, err)
	}

	p.Obj = s

	return nil
}

func dnsCheck(field *string) error {
	dns := letterTest.FindString(*field)
	if dns != "" {
		ip, err := net.LookupIP(*field)
		if err != nil {
			return fmt.Errorf("failed dns lookup: %w", err)
		}

		*field = ip[0].String()
	}

	return nil
}
