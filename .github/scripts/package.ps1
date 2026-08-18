#Requires -Version 5.1
param(
    [Parameter(Mandatory = $true)][string]$Goos,
    [Parameter(Mandatory = $true)][string]$Goarch,
    [Parameter(Mandatory = $true)][string]$Binary,
    [string]$PluginDll
)
$ErrorActionPreference = 'Stop'

$RepoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Push-Location $RepoRoot
try {
    if ($env:GITHUB_REF_TYPE -eq 'tag' -and $env:GITHUB_REF_NAME) {
        $Version = $env:GITHUB_REF_NAME
    }
    else {
        $Version = (& git describe --tags --always --dirty 2>$null)
        if (-not $Version) { $Version = 'dev' }
    }

    $Name = "stdt86-$Version-$Goos-$Goarch"
    $Pkg = Join-Path 'pkg' $Name
    $ItuSrc = 'third_party\T-REC-G.722.1-200505-I!!SOFT-ZST-E\Software\Fixed-200505-Rel.2.1'
    $Zip = "$Name.zip"

    Remove-Item -Recurse -Force 'pkg', $Zip -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force (Join-Path $Pkg 'build') | Out-Null

    Copy-Item -LiteralPath $Binary -Destination (Join-Path $Pkg (Split-Path -Leaf $Binary))

    Copy-Item -Recurse -LiteralPath 'build\g7221' -Destination (Join-Path $Pkg 'build\g7221')
    Copy-Item -LiteralPath (Join-Path $ItuSrc 'Readme.txt') `
        -Destination (Join-Path $Pkg 'build\g7221\ITU-G.722.1-Readme.txt')

    Copy-Item -LiteralPath 'readme.md' -Destination (Join-Path $Pkg 'readme.md')

    if ($PluginDll) {
        if (-not (Test-Path -LiteralPath $PluginDll)) { throw "プラグイン DLL が無い: $PluginDll" }
        $pluginDir = Join-Path $Pkg 'sdrsharp-plugin'
        New-Item -ItemType Directory -Force $pluginDir | Out-Null
        Copy-Item -LiteralPath $PluginDll -Destination $pluginDir
        Copy-Item -LiteralPath 'contrib\sdrsharp-iq-tcp\README.md' -Destination $pluginDir
        $leak = Get-ChildItem $pluginDir -Filter 'SDRSharp.*.dll' |
            Where-Object { $_.Name -ne 'SDRSharp.IqTcpServer.dll' }
        if ($leak) { throw "SDR# 本体のアセンブリが混ざっている: $($leak.Name -join ', ')" }
    }

    Compress-Archive -Path $Pkg -DestinationPath $Zip -CompressionLevel Optimal
    Remove-Item -Recurse -Force 'pkg'

    Write-Host "作成: $Zip"
    Expand-Archive -LiteralPath $Zip -DestinationPath (Join-Path ([IO.Path]::GetTempPath()) "verify-$Name") -Force
    Get-ChildItem -Recurse (Join-Path ([IO.Path]::GetTempPath()) "verify-$Name") |
        Select-Object -ExpandProperty FullName
    Remove-Item -Recurse -Force (Join-Path ([IO.Path]::GetTempPath()) "verify-$Name")
}
finally { Pop-Location }
