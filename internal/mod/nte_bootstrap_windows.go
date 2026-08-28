//go:build windows

package mod

import (
	"fmt"
	"strings"
)

func elevatedCopyNteBootstrapFiles(copies []nteBootstrapFileCopy) error {
	var entries strings.Builder
	for _, file := range copies {
		fmt.Fprintf(&entries, "[pscustomobject]@{ SourcePath = %s; TargetPath = %s }\n", ntePSLiteral(file.sourcePath), ntePSLiteral(file.targetPath))
	}
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$Copies = @(
%s)
foreach ($Copy in $Copies) {
  [System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($Copy.TargetPath)) | Out-Null
  Copy-Item -LiteralPath $Copy.SourcePath -Destination $Copy.TargetPath -Force
}
`, entries.String())
	if _, err := executeNtePowerShell(script, true); err != nil {
		return fmt.Errorf("NTE_BOOTSTRAP_ELEVATED_COPY_FAILED: %w", err)
	}
	return nil
}

func elevatedRollbackNteBootstrapFiles(snapshots []nteBootstrapSnapshot) error {
	var entries strings.Builder
	for _, snapshot := range snapshots {
		existed := "$false"
		if snapshot.existed {
			existed = "$true"
		}
		fmt.Fprintf(&entries, "[pscustomobject]@{ TargetPath = %s; BackupPath = %s; Existed = %s }\n", ntePSLiteral(snapshot.targetPath), ntePSLiteral(snapshot.backupPath), existed)
	}
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$Snapshots = @(
%s)
[array]::Reverse($Snapshots)
foreach ($Snapshot in $Snapshots) {
  if ($Snapshot.Existed) {
    [System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($Snapshot.TargetPath)) | Out-Null
    Copy-Item -LiteralPath $Snapshot.BackupPath -Destination $Snapshot.TargetPath -Force
  } elseif (Test-Path -LiteralPath $Snapshot.TargetPath) {
    Remove-Item -LiteralPath $Snapshot.TargetPath -Force
  }
}
`, entries.String())
	if _, err := executeNtePowerShell(script, true); err != nil {
		return fmt.Errorf("NTE_BOOTSTRAP_ELEVATED_ROLLBACK_FAILED: %w", err)
	}
	return nil
}
