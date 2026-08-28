#Requires -Version 5.1

$script:WinLibsRepo = 'brechtsanders/winlibs_mingw'
$script:WingetIds = @(
    'BrechtSanders.WinLibs.POSIX.UCRT',
    'BrechtSanders.WinLibs.POSIX.MSVCRT',
    'BrechtSanders.WinLibs.MCF.UCRT',
    'BrechtSanders.WinLibs.MCF.MSVCRT'
)

function Expand-ZipFast([string]$Zip, [string]$Destination) {
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    if (Test-Path -LiteralPath $Destination) { Remove-Item -Recurse -Force $Destination }
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    [IO.Compression.ZipFile]::ExtractToDirectory(
        (Resolve-Path -LiteralPath $Zip).Path,
        (Resolve-Path -LiteralPath $Destination).Path)
}

function Sync-PathFromEnvironment {
    $parts = @()
    foreach ($scope in @('Machine', 'User')) {
        $v = [Environment]::GetEnvironmentVariable('Path', $scope)
        if ($v) { $parts += $v }
    }
    $links = Join-Path $env:LOCALAPPDATA 'Microsoft\WinGet\Links'
    if (Test-Path -LiteralPath $links) { $parts += $links }
    if ($parts.Count -gt 0) { $env:Path = ($parts -join ';') }
}

function Find-CCompiler {
    if ($env:CC) {
        $cmd = Get-Command $env:CC -ErrorAction SilentlyContinue
        if ($cmd) { return $cmd.Source }
        if (Test-Path -LiteralPath $env:CC) { return (Resolve-Path -LiteralPath $env:CC).Path }
        throw "CC に指定されたコンパイラが見つかりません: $env:CC"
    }
    foreach ($name in @('gcc', 'clang')) {
        $cmd = Get-Command $name -ErrorAction SilentlyContinue
        if ($cmd) { return $cmd.Source }
    }
    return $null
}

function Find-GccUnderWinGet {
    $packages = Join-Path $env:LOCALAPPDATA 'Microsoft\WinGet\Packages'
    if (-not (Test-Path -LiteralPath $packages)) { return $null }
    $dirs = Get-ChildItem -LiteralPath $packages -Directory -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -like '*WinLibs*' -or $_.Name -like '*mingw*' }
    foreach ($d in $dirs) {
        $hit = Get-ChildItem -LiteralPath $d.FullName -Filter 'gcc.exe' -Recurse -File -ErrorAction SilentlyContinue |
            Select-Object -First 1
        if ($hit) { return $hit.FullName }
    }
    return $null
}

function Install-GccWithWinget {
    $cc = Find-GccUnderWinGet
    if ($cc) {
        Write-Host "    winget で入れた gcc がありました: $cc"
        return $cc
    }
    $winget = Get-Command winget -ErrorAction SilentlyContinue
    if (-not $winget) {
        Write-Host '    winget がありません（Windows 10 1809 未満など）。ポータブル版へ切り替えます。'
        return $null
    }
    foreach ($id in $script:WingetIds) {
        Write-Host "    winget install $id"
        & $winget.Source install --id $id --exact --source winget `
            --accept-source-agreements --accept-package-agreements `
            --disable-interactivity 2>&1 | Write-Host
        $installed = ($LASTEXITCODE -eq 0)
        Sync-PathFromEnvironment
        $cc = Find-CCompiler
        if (-not $cc) { $cc = Find-GccUnderWinGet }
        if ($cc) { return $cc }
        if ($installed) {
            Write-Host '    インストールは成功しましたが gcc.exe を見つけられませんでした。'
        }
    }
    return $null
}

function Install-GccPortable([string]$ToolchainDir) {
    $api = "https://api.github.com/repos/$script:WinLibsRepo/releases/latest"
    Write-Host "    WinLibs の最新 Release を照会: $api"
    $release = Invoke-RestMethod -Uri $api -UserAgent 'arib-build' -Headers @{ Accept = 'application/vnd.github+json' }
    $asset = $release.assets |
        Where-Object { $_.name -like '*.zip' } |
        Where-Object { $_.name -like '*x86_64*' } |
        Where-Object { $_.name -like '*posix*' } |
        Where-Object { $_.name -like '*ucrt*' } |
        Where-Object { $_.name -notlike '*llvm*' } |
        Sort-Object -Property size |
        Select-Object -First 1
    if (-not $asset) { throw "WinLibs の zip が Release に見つかりません（$api）" }

    New-Item -ItemType Directory -Force -Path $ToolchainDir | Out-Null
    $zip = Join-Path $ToolchainDir $asset.name
    if (-not (Test-Path -LiteralPath $zip)) {
        Write-Host ("    ダウンロード: {0} （{1:N0} MB）" -f $asset.name, ($asset.size / 1MB))
        Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zip -UserAgent 'arib-build'
    }
    $dest = Join-Path $ToolchainDir 'winlibs'
    Write-Host '    展開（数分かかることがあります）'
    Expand-ZipFast -Zip $zip -Destination $dest

    $hit = Get-ChildItem -LiteralPath $dest -Filter 'gcc.exe' -Recurse -File -ErrorAction SilentlyContinue |
        Select-Object -First 1
    if (-not $hit) { throw "展開しましたが gcc.exe が見つかりません: $dest" }
    $env:Path = (Split-Path -Parent $hit.FullName) + ';' + $env:Path
    return $hit.FullName
}

function Resolve-CCompiler([string]$ToolchainDir, [switch]$NoInstall) {
    $cc = Find-CCompiler
    if ($cc) {
        Write-Host "    コンパイラ: $cc"
        return $cc
    }
    if ($NoInstall) {
        throw 'gcc / clang が見つかりません。MinGW-w64 を入れるか $env:CC で指定してください（MSVC の cl は非対応）。'
    }
    Write-Host '    gcc が無いので MinGW-w64（WinLibs）を入れます。'
    $cc = Install-GccWithWinget
    if (-not $cc) { $cc = Install-GccPortable $ToolchainDir }
    Write-Host "    用意しました: $cc"
    return $cc
}
