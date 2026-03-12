package approval

import "errors"

// MultiNotifier wraps multiple notifiers and sends to all of them.
type MultiNotifier struct {
	notifiers []Notifier
}

// NewMultiNotifier creates a notifier that sends to all wrapped notifiers.
func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier {
	return &MultiNotifier{notifiers: notifiers}
}

// Notify sends the approval request to all wrapped notifiers, joining any errors.
func (m *MultiNotifier) Notify(req *PendingRequest, callbackBaseURL string, token string) error {
	var errs []error
	for _, n := range m.notifiers {
		if err := n.Notify(req, callbackBaseURL, token); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
