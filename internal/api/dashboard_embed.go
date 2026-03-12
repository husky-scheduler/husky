package api

import "embed"

// dashboardAssets contains the compiled dashboard bundle served by the API server.
//
//go:embed dashboard
var dashboardAssets embed.FS
