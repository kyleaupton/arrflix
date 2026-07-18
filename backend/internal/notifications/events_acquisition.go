package notifications

import (
	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
)

// WantAvailable — audience: user, bundle: my_requests. Emitted by acquisition when
// a tracked want reaches the 'available' terminal state: the requested title is
// ready to watch. Recipient is the requesting user; PlexLink deep-links to play.
type WantAvailable struct {
	Recipient uuid.UUID `json:"-"`
	Media     MediaRef  `json:"media"`
	PlexLink  string    `json:"plexLink,omitempty"`
}

func (e WantAvailable) EventType() string                    { return "want.available" }
func (e WantAvailable) Audience() model.NotificationAudience { return model.AudienceUser }
func (e WantAvailable) Bundle() string                       { return BundleMyRequests }
func (e WantAvailable) Recipients() []uuid.UUID              { return []uuid.UUID{e.Recipient} }
func (e WantAvailable) DedupKey() string                     { return "" }
func (e WantAvailable) Payload() any                         { return e }

var _ Event = WantAvailable{}
