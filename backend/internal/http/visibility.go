package http

import (
	"context"

	"github.com/google/uuid"

	"github.com/AGlElectronics/tasks/backend/internal/auth"
	"github.com/AGlElectronics/tasks/backend/internal/domain"
	"github.com/AGlElectronics/tasks/backend/internal/tasks"
)

// visibility is what a person can see across all spaces, computed with the
// same rules as requireSpace/requireList (see taskHandler). It feeds
// cross-space queries (search, calendar) and the live-update stream.
type visibility struct {
	// viewSpaces: spaces at view or edit (private lists excluded);
	// manageSpaces: spaces at manage (everything); grantedLists: lists
	// shared directly. Together with "assigned to me" these are the SQL
	// filter for cross-space task queries.
	viewSpaces, manageSpaces, grantedLists []uuid.UUID
	// spaces and lists are the same as sets, for the stream; lists also
	// includes lists holding tasks the person is assigned to or watches.
	spaces map[uuid.UUID]bool
	lists  map[uuid.UUID]bool
}

func (h *taskHandler) visibility(ctx context.Context, u *auth.User, includeArchived bool) (visibility, error) {
	v := visibility{spaces: map[uuid.UUID]bool{}, lists: map[uuid.UUID]bool{}}
	spaces, _, err := h.svc.ListSpaces(ctx, includeArchived)
	if err != nil {
		return v, err
	}
	acc, err := h.svc.AccessFor(ctx, u.ID, u.Groups)
	if err != nil {
		return v, err
	}
	for _, sp := range spaces {
		level := tasks.LevelNone
		if sp.EveryoneLevel != nil {
			level = tasks.ParseLevel(*sp.EveryoneLevel)
		}
		level = level.Max(acc.SpaceLevels[sp.ID])
		if u.HasInOrg(domain.PermSpaceManage, sp.OrgID) ||
			(u.HasInOrg(domain.PermSpaceCreate, sp.OrgID) && sp.CreatedBy != nil && *sp.CreatedBy == u.ID) {
			level = tasks.LevelManage
		}
		switch {
		case level >= tasks.LevelManage:
			v.manageSpaces = append(v.manageSpaces, sp.ID)
		case level >= tasks.LevelView:
			v.viewSpaces = append(v.viewSpaces, sp.ID)
		}
		for _, l := range sp.Lists {
			if (level >= tasks.LevelManage) || (level >= tasks.LevelView && !l.Private) || acc.ListLevels[l.ID] >= tasks.LevelView {
				v.lists[l.ID] = true
				v.spaces[sp.ID] = true
			}
		}
		if level >= tasks.LevelView {
			v.spaces[sp.ID] = true
		}
	}
	for id, level := range acc.ListLevels {
		if level >= tasks.LevelView {
			v.grantedLists = append(v.grantedLists, id)
		}
	}
	mine, err := h.svc.ListsWithMyTasks(ctx, u.ID)
	if err != nil {
		return v, err
	}
	for _, id := range mine {
		v.lists[id] = true
	}
	return v, nil
}
