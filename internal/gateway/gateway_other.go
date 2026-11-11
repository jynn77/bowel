//go:build !darwin && !linux && !windows

package gateway

import (
	"errors"
	"time"

	"github.com/oboo/moho/internal/tun"
)

var errUnsupported = errors.New("not supported")

// OS is this system's: one bowel has no gateway for only reads the
// network.
func OS() System { return other{} }

// Supported says whether this system can be made a gateway.
func Supported() bool { return false }

// PrimaryInterface is unknown here.
func PrimaryInterface() string { return "" }

func onLAN(name string) bool { return physical(name) }

type other struct{}

func (other) Forwarding() (bool, error)         { return false, errUnsupported }
func (other) SetForwarding(bool) error          { return errUnsupported }
func (other) Ready() bool                       { return false }
func (other) Prepare(string, ...tun.Step) error { return errUnsupported }
func (other) KeepAwake() (func(), error)        { return nil, errUnsupported }
func (other) Network() Network                  { return scanLAN() }
func (other) AnswersDNS(addr string) bool       { return answersDNS(addr, time.Second) }
