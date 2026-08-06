package server

import "net/http"

type access int

const (
	unguarded access = iota
	owner
	ownerOrApprovedDevice
	approvedDevice
)

type sealing int

const (
	unsealed sealing = iota
	sealedByHeader
	sealedByQuery
)

type route struct {
	pattern string
	access  access
	sealing sealing
	serve   func(*server, http.ResponseWriter, *http.Request)
}

func routes() []route {
	return []route{
		{"GET /api/health", unguarded, unsealed, (*server).health},
		{"GET /api/session", unguarded, unsealed, (*server).session},
		{"POST /api/pair", unguarded, unsealed, (*server).pair},
		{"POST /api/seal", approvedDevice, unsealed, (*server).handshake},
		{"GET /api/pairing", owner, unsealed, (*server).currentPairing},
		{"POST /api/pairing", owner, unsealed, (*server).rotatePairing},
		{"GET /api/devices", owner, unsealed, (*server).devices},
		{"POST /api/devices/{deviceId}/approve", owner, unsealed, (*server).approveDevice},
		{"DELETE /api/devices/{deviceId}", unguarded, unsealed, (*server).revokeDevice},
		{"GET /api/transfers", ownerOrApprovedDevice, sealedByHeader, (*server).transfers},
		{"DELETE /api/transfers", ownerOrApprovedDevice, unsealed, (*server).clearTransfers},
		{"DELETE /api/transfers/{transferId}", ownerOrApprovedDevice, unsealed, (*server).deleteTransfer},
		{"POST /api/received/open", owner, unsealed, (*server).openReceivedFolder},
		{"GET /api/files", ownerOrApprovedDevice, sealedByHeader, (*server).files},
		{"POST /api/files", owner, unsealed, (*server).offerFiles},
		{"POST /api/files/pick", owner, unsealed, (*server).pickFiles},
		{"DELETE /api/files/{fileId}", ownerOrApprovedDevice, unsealed, (*server).removeFile},
		{"GET /api/files/{fileId}/content", ownerOrApprovedDevice, unsealed, (*server).downloadFile},
		{"GET /api/events", unguarded, sealedByQuery, (*server).events},
		{"/api/uploads/", approvedDevice, sealedByHeader, (*server).upload},
		{"GET /api/settings", owner, unsealed, (*server).settings},
		{"PATCH /api/settings", owner, unsealed, (*server).patchSettings},
		{"POST /api/settings/received-dir/pick", owner, unsealed, (*server).pickReceivedDir},
		{"GET /api/update", owner, unsealed, (*server).updateStatus},
		{"POST /api/update/check", owner, unsealed, (*server).checkUpdate},
		{"POST /api/update/apply", owner, unsealed, (*server).applyUpdate},
		{"GET /api/network", owner, unsealed, (*server).network},
		{"POST /api/network/allow", owner, unsealed, (*server).allowNetwork},
		{"POST /api/app/show", owner, unsealed, (*server).showWindow},
		{"POST /api/app/quit", owner, unsealed, (*server).quit},
		{"GET /manifest.webmanifest", unguarded, unsealed, (*server).manifest},
	}
}

func Routes() []string {
	table := routes()
	patterns := make([]string, len(table))
	for i, rt := range table {
		patterns[i] = rt.pattern
	}
	return patterns
}
