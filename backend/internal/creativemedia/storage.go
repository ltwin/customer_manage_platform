// Package creativemedia owns upload sessions, verification, publication and
// authorized reads of image/video/audio bytes for the creative namespace.
// The object bytes themselves live behind the platform's versionedfs port.
package creativemedia

import (
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

// Five of these conditions are raised by both this package and the object
// port, and the API edge maps them to one status code each. Adopting the
// port's values keeps a single identity per condition, so errors.Is at the
// edge does not need to know whether the cause was storage or domain state.
var (
	ErrNotFound      = versionedfs.ErrNotFound
	ErrState         = versionedfs.ErrState
	ErrSizeLimit     = versionedfs.ErrSizeLimit
	ErrRange         = versionedfs.ErrRange
	ErrUnknownResult = versionedfs.ErrUnknownResult

	ErrExpired     = errors.New("creative upload expired")
	ErrUnsupported = errors.New("creative media format unsupported")
	ErrQuota       = errors.New("creative media quota exceeded")
	ErrTicket      = errors.New("creative media ticket invalid")
	ErrEpoch       = errors.New("creative upload execution epoch is stale")
)
