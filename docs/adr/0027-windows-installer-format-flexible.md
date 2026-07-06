# Produce a Windows x64 installer without locking the installer format

Echo v0.1 will ship as a Windows x64 installer uploaded to GitHub Releases by GitHub Actions. The project will use the least-customized installer format that fits the Wails 3 build and packaging chain, whether that is NSIS, MSI, or another supported Windows installer output. The product requirement is a normal double-click installer with versioned release assets; MVP does not require a portable build, auto-update, or a specific installer technology.
