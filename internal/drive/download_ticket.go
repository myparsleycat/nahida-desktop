package drive

import (
	"context"
	"errors"
	"net/http"
)

// ModTicketRedemptionError identifies failures before a ticket can be redeemed.
type ModTicketRedemptionError struct {
	err error
}

func (e *ModTicketRedemptionError) Error() string { return e.err.Error() }

func (e *ModTicketRedemptionError) Unwrap() error { return e.err }

type modTicketRedemption struct {
	ItemID string
	Name   string
	Grant  string
}

func (d *Drive) redeemModDownloadTicket(ctx context.Context, ticket string) (modTicketRedemption, error) {
	data, apiErr, err := d.doJSON(
		ctx,
		http.MethodPost,
		"/akasha/mod/download-ticket/redeem",
		nil,
		map[string]string{"ticket": ticket},
	)
	if err != nil {
		return modTicketRedemption{}, err
	}
	if apiErr != nil {
		return modTicketRedemption{}, CreateDriveAPIError(apiErr.asAny(), "redeem mod download ticket", apiErr.Status)
	}
	payload, ok := asRecord(data)
	if !ok {
		return modTicketRedemption{}, errors.New("invalid mod download ticket response")
	}
	id, idOK := payload["itemId"].(string)
	name, nameOK := payload["name"].(string)
	grant, grantOK := payload["grant"].(string)
	isDir, dirOK := payload["isDir"].(bool)
	if !idOK || id == "" || !nameOK || name == "" || !grantOK || len(grant) != 43 || !dirOK || !isDir {
		return modTicketRedemption{}, errors.New("invalid mod download ticket response")
	}
	return modTicketRedemption{ItemID: id, Name: name, Grant: grant}, nil
}
