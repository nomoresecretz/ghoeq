package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/nomoresecretz/ghoeq-common/eqStruct"
	"github.com/nomoresecretz/ghoeq/server/assembler"
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
	client  *gameClient
	sType   streamType
	dir     assembler.FlowDirection
}

type streamPredict map[string]predictEntry

type gameClientWatch struct {
	mu        sync.RWMutex
	clients   map[uuid.UUID]*gameClient
	predicted streamPredict
	ping      chan struct{}
	decoder   opDecoder
	charMap   map[string]*gameClient
	acctMap   map[string]*gameClient
}

type gameClient struct {
	id              uuid.UUID
	mu              sync.RWMutex
	streams         map[assembler.Key]*stream
	epoch           atomic.Uint32
	ping            chan struct{}
	decoder         opDecoder
	parent          *gameClientWatch
	clientServer    string                  // game server name.
	clientSessionID string                  // Assigned by server.
	clientCharacter string                  // game client character.
	PlayerProfile   *eqStruct.PlayerProfile // temporary for testing.
}

func NewPredict(gc *gameClient, s streamType, dir assembler.FlowDirection) predictEntry {
	return predictEntry{
		created: now(),
		client:  gc,
		sType:   s,
		dir:     dir,
	}
}

func NewClientWatch() *gameClientWatch {
	return &gameClientWatch{
		clients:   make(map[uuid.UUID]*gameClient),
		ping:      make(chan struct{}),
		predicted: make(streamPredict),
		charMap:   make(map[string]*gameClient),
		acctMap:   make(map[string]*gameClient),
	}
}

func (c *gameClientWatch) newClient() *gameClient {
	gc := &gameClient{
		id:      uuid.New(),
		streams: make(map[assembler.Key]*stream),
		ping:    make(chan struct{}),
		decoder: c.decoder,
		parent:  c,
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.clients[gc.id] = gc
	close(c.ping)
	c.ping = make(chan struct{})

	slog.Debug("new game client tracked", "client", gc.id)

	return gc
}

func (c *gameClient) AddStream(s *stream) {
	slog.Debug("gameClient stream added")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streams[s.key] = s
	c.LockedPing()
}

func (c *gameClient) LockedPing() {
	ping := c.ping
	c.ping = make(chan struct{})

	close(ping)
}

func (c *gameClientWatch) CheckFull(p *StreamPacket, pr predictEntry, b bool) bool {
	invalid := time.Now().Add(maxPredictTime)
	if !b || pr.created.After(invalid) {
		return false
	}

	p.stream.dir = pr.dir
	p.stream.sType = pr.sType
	p.stream.gameClient = pr.client

	if pr.client == nil {
		return true
	}

	pr.client.AddStream(p.stream)

	return true
}

func (c *gameClientWatch) CheckReverse(p *StreamPacket, rpr predictEntry, b bool) bool {
	invalid := time.Now().Add(maxPredictTime)
	if !b || rpr.created.After(invalid) {
		return false
	}

	p.stream.dir = rpr.dir.Reverse()
	p.stream.sType = rpr.sType
	p.stream.gameClient = rpr.client

	if rpr.client == nil {
		return true
	}

	rpr.client.AddStream(p.stream)

	return true
}

func (c *gameClientWatch) CheckHalf(p *StreamPacket) bool {
	testKey := fmt.Sprintf("dst-%s:%s", p.stream.net.Dst().String(), p.stream.port.Dst().String())
	testKeyR := fmt.Sprintf("src-%s:%s", p.stream.net.Src().String(), p.stream.port.Src().String())

	c.mu.RLock()
	prF, okF := c.predicted[testKey]
	prB, okB := c.predicted[testKeyR]
	c.mu.RUnlock()

	if okF {
		p.stream.dir = prF.dir
		p.stream.sType = prF.sType
		p.stream.gameClient = prF.client

		if prF.client == nil {
			return true
		}

		prF.client.AddStream(p.stream)

		return true
	}

	if okB {
		p.stream.dir = prB.dir
		p.stream.sType = prB.sType
		p.stream.gameClient = prB.client

		if prB.client == nil {
			return true
		}

		prB.client.AddStream(p.stream)

		return true
	}

	return false
}

func (c *gameClientWatch) Check(p *StreamPacket) bool {
	c.mu.RLock()
	pr, ok1 := c.predicted[p.stream.key.String()]
	rv := p.stream.key.Reverse()
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

func (c *gameClientWatch) newPredictClient(p *StreamPacket) {
	rv := p.stream.key.Reverse()
	cli := c.newClient()
	slog.Info("unknown/new client thread seen, adding client", "client", cli.id, "streamtype", p.stream.sType.String())
	p.stream.gameClient = cli
	cli.AddStream(p.stream)
	c.predicted[rv.String()] = NewPredict(cli, p.stream.sType, p.stream.dir.Reverse())
}

func (gw *gameClientWatch) GracefulStop() {
	for _, gc := range gw.clients {
		gc.Close()
	}

	gw.mu.Lock()
	defer gw.mu.Unlock()
	close(gw.ping)
	gw.ping = nil
}

func (c *gameClientWatch) charMatch(n string) *gameClient {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, gc := range c.clients {
		if gc.clientCharacter == n {
			return gc
		}
	}

	return nil
}

func (c *gameClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	close(c.ping)
	c.ping = nil
	c.parent.mu.Lock()
	delete(c.parent.charMap, c.clientCharacter)
	c.parent.mu.Unlock()
}

func (c *gameClient) Run(p *StreamPacket) error {
	op := c.decoder.GetOp(p.opCode)

	switch op {
	case "":
		slog.Debug("game client unknown opcode", "opcode", p.opCode.String())
	case "OP_PlayEverquestRequest":
		return c.handlePlayEQ(p)
	case "OP_LogServer":
		if p.stream.dir == assembler.DirServerToClient {
			return c.handleLogServer(p)
		}
	case "OP_PlayerProfile":
		if p.stream.dir == assembler.DirServerToClient {
			return handleOpCode(&eqStruct.PlayerProfile{}, p)
		}
	case "OP_EnterWorld":
		return c.handleEnterWorld(p)
	case "OP_ZoneServerInfo":
		return c.handleZoneServerInfo(p)
	case "OP_ZoneEntry":
		switch p.stream.dir {
		case assembler.DirServerToClient:
			return c.handleZoneEntryServer(p)
		case assembler.DirClientToServer:
			return c.handleZoneEntryClient(p)
		default:
			slog.Info("unhandled type: ", "type", op, "dir", p.stream.dir)
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
		return handleRaidUpdate(p)
	case "OP_ApproveWorld":
		return handleOpCode(&eqStruct.WorldApprove{}, p)
	case "OP_GuildsList":
		return handleOpCode(&eqStruct.GuildsList{}, p)
	case "OP_GuildUpdate":
		return handleOpCode(&eqStruct.GuildUpdate{}, p)
	case "OP_SendZonepoints":
		return handleOpCode(&eqStruct.ZonePoints{}, p)
	case "OP_LFGCommand":
		return handleLFGCommand(p)
	case "OP_Weather":
		return handleOpCode(&eqStruct.Weather{}, p)
	case "OP_TimeOfDay":
		return handleOpCode(&eqStruct.Time{}, p)
	case "OP_SpawnDoor":
		return handleOpCode(&eqStruct.SpawnDoors{}, p)
	case "OP_ChannelMessage":
		return handleOpCode(&eqStruct.ChannelMessage{}, p)
	case "OP_ReqNewZone":
	case "OP_SetChatServer":
	case "OP_ChecksumSpell":
	case "OP_ChecksumExe":
	case "OP_ExpansionInfo":
	case "OP_ZoneEntryResend":
	case "OP_SendCharInfo":
	case "OP_DataRate":
	default:
		slog.Info("unhandled type: ", "type", op, "dir", p.stream.dir)
	}

	return nil
}

func (gw *gameClientWatch) matchChar(n string) *gameClient {
	gw.mu.RLock()
	defer gw.mu.RUnlock()

	// TODO: be server aware.
	if c, ok := gw.charMap[n]; ok {
		return c
	}

	return nil
}

func (gw *gameClientWatch) matchAcct(n string) *gameClient {
	gw.mu.RLock()
	defer gw.mu.RUnlock()

	// TODO: be server aware.
	if c, ok := gw.acctMap[n]; ok {
		return c
	}

	return nil
}

func (gw *gameClientWatch) RegisterCharacter(c *gameClient) error {
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

func (gw *gameClientWatch) RegisterAcct(c *gameClient) error {
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

func (c *gameClient) matchChar(n string) bool {
	gc := c.parent.matchChar(n)
	if gc == nil || gc == c {
		return false
	}
	slog.Info("matched extra client by charName, reducing", "name", n)

	c.reduce(gc)

	return true
}

func (c *gameClient) matchAcct(n string) bool {
	gc := c.parent.matchAcct(n)
	if gc == nil || gc == c {
		return false
	}
	slog.Info("matched extra client by account, reducing", "name", n)

	c.reduce(gc)
	return true
}

func (c *gameClient) reduce(gc *gameClient) {
	gc.mu.Lock()
	c.mu.Lock()

	for _, s := range c.streams {
		s.gameClient = gc
		gc.streams[s.key] = s
		delete(c.streams, s.key)
	}

	c.parent.mu.Lock()
	delete(c.parent.clients, c.id)
	gc.LockedPing()
	c.parent.mu.Unlock()
	c.mu.Unlock()
	gc.mu.Unlock()
}

func (c *gameClientWatch) Run(p *StreamPacket) error {
	if c.Check(p) {
		slog.Debug("success matching unknown flow", "stream", p.stream.key.String())

		if err := p.stream.gameClient.Run(p); err != nil {
			return err
		}
		return nil
	}

	c.Check(p)

	if p.stream.sType == ST_UNKNOWN {
		p.stream.Identify(p.packet)
	}

	if c.Check(p) {
		slog.Debug("fallback matching unknown flow", "stream", p.stream.key.String())

		if err := p.stream.gameClient.Run(p); err != nil {
			return err
		}
	}

	switch p.stream.sType {
	case ST_WORLD, ST_LOGIN: // Wait for a zone event at minimum.
		c.newPredictClient(p)
		return p.stream.gameClient.Run(p)
	}

	return nil
}

func (c *gameClientWatch) getFirstClient() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for k := range c.clients {
		return k.String()
	}

	return ""
}

// WaitForClient obtains a named client, or can block waiting for first avaliable.
func (c *gameClientWatch) WaitForClient(ctx context.Context, id string) (*gameClient, error) {
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

func (c *gameClient) PingChan() chan struct{} {
	c.mu.RLock()
	p := c.ping
	c.mu.RUnlock()
	return p
}

func (c *gameClient) handlePlayEQ(p *StreamPacket) error {
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
	c.parent.predicted[fKey] = NewPredict(c, ST_WORLD, assembler.DirClientToServer)
	c.parent.predicted[rKey] = NewPredict(c, ST_WORLD, assembler.DirServerToClient)
	c.parent.mu.Unlock()

	return nil
}

func (c *gameClient) handleLogServer(p *StreamPacket) error {
	ls := &eqStruct.LogServer{}
	if err := handleOpCode(ls, p); err != nil {
		return err
	}

	if c.clientServer == "" {
		c.clientServer = ls.ShortName
	}

	return nil
}

func (c *gameClient) handleEnterWorld(p *StreamPacket) error {
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

func (c *gameClient) handleZoneServerInfo(p *StreamPacket) error {
	s := &eqStruct.ZoneServerInfo{}
	if err := handleOpCode(s, p); err != nil {
		return err
	}

	slog.Debug("zoning event detected", "client", c.id)

	if err := dnsCheck(&s.IP); err != nil {
		return fmt.Errorf("zone server dns lookup failure: %w", err)
	}

	Key := fmt.Sprintf("%s:%d", s.IP, s.Port)
	fKey, rKey := "dst-"+Key, "src-"+Key

	c.parent.mu.Lock()
	c.parent.predicted[fKey] = NewPredict(c, ST_ZONE, assembler.DirClientToServer)
	c.parent.predicted[rKey] = NewPredict(c, ST_ZONE, assembler.DirServerToClient)
	c.parent.mu.Unlock()

	return nil
}

func (c *gameClient) handleZoneEntryServer(p *StreamPacket) error {
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

func (c *gameClient) handleZoneEntryClient(p *StreamPacket) error {
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

func (c *gameClient) handleSendLoginInfo(p *StreamPacket) error {
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

func (c *gameClient) handleLoginAccept(p *StreamPacket) error {
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

func handleRaidUpdate(p *StreamPacket) error {
	switch len(p.packet.Payload) { // Hacky way to avoid tracking raid state
	case lenRaidCreate:
		return handleOpCode(&eqStruct.RaidCreate{}, p)
	case lenRaidAddMember:
		return handleOpCode(&eqStruct.RaidAddMember{}, p)
	case lenRaidGeneral:
		return handleOpCode(&eqStruct.RaidGeneral{}, p)
	default:
		slog.Info("unhandled type: ", "type", "OP_RaidUpdate", "len", len(p.packet.Payload), "dir", p.stream.dir)
	}

	return nil
}

func handleLFGCommand(p *StreamPacket) error {
	switch len(p.packet.Payload) { // Hacky way to avoid tracking raid state
	case lenLFGAppearance:
		return handleOpCode(&eqStruct.LFGAppearance{}, p)
	case lenLFGCommand:
		return handleOpCode(&eqStruct.LFG{}, p)
	default:
		slog.Info("unhandled type: ", "type", "OP_RaidUpdate", "len", len(p.packet.Payload), "dir", p.stream.dir)
	}

	return nil
}

func handleOpCode[T eqStruct.EQStruct](s T, p *StreamPacket) error {
	if _, err := s.Unmarshal(p.packet.Payload); err != nil {
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
