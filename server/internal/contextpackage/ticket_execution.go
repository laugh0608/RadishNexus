package contextpackage

import "sort"

func validateDiscussionHistory(d Data, x *Index, links map[string]Link) error {
	type pair struct{ ticket, thread string }
	groups := map[pair][]Link{}
	removed := map[string]bool{}
	for _, h := range d.History {
		if !oneOf(h.Type, "ticket.discussion-linked", "ticket.discussion-unlinked") {
			continue
		}
		f := x.Facts[h.ID]
		l, ok := links[f.Relation.ID]
		if !ok || l.From != h.Entity || l.To != *f.Thread {
			return invalid("Ticket discussion event target")
		}
		if h.Type == "ticket.discussion-linked" {
			if l.EventID != h.ID {
				return invalid("duplicate Ticket discussion creation")
			}
		} else {
			if removed[l.ID] || l.State != "removed" || l.RemovedAt == nil || !h.OccurredAt.Equal(*l.RemovedAt) ||
				!l.UpdatedAt.Equal(*l.RemovedAt) || !l.RemovedAt.After(l.CreatedAt) || h.Actor != *l.RemovedActor || *l.RemovalReason != "ticket-discussion-unlinked" {
				return invalid("Ticket discussion removal history")
			}
			removed[l.ID] = true
		}
	}
	for _, l := range links {
		if (l.State == "removed" && !removed[l.ID]) || (l.State == "active" && !l.UpdatedAt.Equal(l.CreatedAt)) {
			return invalid("Ticket discussion state")
		}
		k := pair{l.From.ID, l.To.ID}
		groups[k] = append(groups[k], l)
	}
	for _, lines := range groups {
		sort.Slice(lines, func(i, j int) bool { return lines[i].CreatedAt.Before(lines[j].CreatedAt) })
		for i := 1; i < len(lines); i++ {
			end := lines[i-1].RemovedAt
			if end == nil || !end.Before(lines[i].CreatedAt) {
				return invalid("overlapping Ticket discussions")
			}
		}
	}
	return nil
}
