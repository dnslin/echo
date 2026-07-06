# Use independent Go modules with a root workspace

Echo will keep the Wails desktop app and Gin business service as separate Go modules, with `apps/desktop/go.mod` and `services/api/go.mod`, coordinated locally by a root `go.work`. The desktop module and API module must not import each other's internal packages; shared Go packages or generated clients can be introduced later only after API contracts stabilize. This keeps the two deployable programs independent while preserving convenient local development from the repository root.
