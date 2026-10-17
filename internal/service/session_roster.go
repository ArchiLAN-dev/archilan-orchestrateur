package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Story 9.56: a generated multiworld no longer carries an injected "Bridge" observer slot. The bridge
// attaches to the first real slot instead - the way it already did for a seed generated elsewhere
// (story 16.18) - so every launch needs the roster. It is read from the session's own generated output,
// once, then stored with the server options so a relaunch-from-save reuses it.

// multidataSlotGroup is Archipelago's SlotType.group: an item-link pseudo-slot nothing can connect as.
const multidataSlotGroup = 2

var errEmptyRoster = errors.New("the multiworld has no slot to attach to")

// rosterFromMultidata turns read_multidata.py's slot table into the roster the bridge reads, in slot
// order and without item-link groups. A session generated before story 9.56 keeps its observer as slot 1,
// so the bridge goes on attaching to it.
func rosterFromMultidata(raw []byte) ([]SlotName, error) {
	var table struct {
		Slots []struct {
			Slot int    `json:"slot"`
			Name string `json:"name"`
			Game string `json:"game"`
			Type int    `json:"type"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(raw, &table); err != nil {
		return nil, fmt.Errorf("read slot table: %w", err)
	}
	sort.SliceStable(table.Slots, func(i, j int) bool { return table.Slots[i].Slot < table.Slots[j].Slot })

	roster := make([]SlotName, 0, len(table.Slots))
	for _, slot := range table.Slots {
		if slot.Type == multidataSlotGroup || slot.Name == "" {
			continue
		}
		roster = append(roster, SlotName{Name: slot.Name, Game: slot.Game})
	}
	if len(roster) == 0 {
		return nil, errEmptyRoster
	}
	return roster, nil
}

// sessionRoster reads the roster out of the output the session's generation left in its volume.
func (s *Service) sessionRoster(ctx context.Context, sessionID string) ([]SlotName, error) {
	dirTar, err := s.docker.CopyOutputDirFromVolume(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("copy output: %w", err)
	}
	archive, err := buildOutputArtifact(dirTar)
	if err != nil {
		return nil, fmt.Errorf("build output archive: %w", err)
	}
	raw, err := s.docker.ReadMultidata(ctx, archive, "archive.zip")
	if err != nil {
		return nil, fmt.Errorf("read multidata: %w", err)
	}
	return rosterFromMultidata(raw)
}

// withRoster fills the launch request's roster from the generated output when the API sent none (a seed
// we generated), and stores it with the server options. A failure is logged and leaves the roster empty:
// the bridge then refuses to start rather than attach to a slot that does not exist.
func (s *Service) withRoster(ctx context.Context, req LaunchRequest) LaunchRequest {
	if len(req.SlotNames) > 0 {
		return req
	}
	roster, err := s.sessionRoster(ctx, req.SessionID)
	if err != nil {
		s.log.Error("launch: could not read the multiworld roster for the bridge", "session_id", req.SessionID, "err", err)
		return req
	}
	req.SlotNames = roster
	if blob, err := marshalServerOptions(req); err != nil {
		s.log.Warn("launch: could not store the roster", "session_id", req.SessionID, "err", err)
	} else if err := s.db.SaveSessionServerOptions(req.SessionID, blob); err != nil {
		s.log.Warn("launch: could not store the roster", "session_id", req.SessionID, "err", err)
	}
	return req
}

// archiveRoster reads the roster of a pre-generated output archive (a launch from file).
func (s *Service) archiveRoster(ctx context.Context, archive []byte, filename string) ([]SlotName, error) {
	raw, err := s.docker.ReadMultidata(ctx, archive, filename)
	if err != nil {
		return nil, fmt.Errorf("read multidata: %w", err)
	}
	return rosterFromMultidata(raw)
}

// hasObserverSlot reports whether a multiworld was generated with the injected "Bridge" observer, which always
// sorted first (story 9.56 removed it for the games generated after).
func hasObserverSlot(roster []SlotName) bool {
	return len(roster) > 0 && roster[0].Name == "Bridge" && roster[0].Game == "Archipelago"
}
