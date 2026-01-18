--- install.ps1.orig
+++ install.ps1
@@
-.PARAMETER Version
-    Specific version to install (e.g., "v1.0.0"). Default: latest.
+.PARAMETER Version
+    Specific version to install (e.g., "v2.1.3"). Default: v2.1.3 (pinned).
@@
-.EXAMPLE
-    # Default install (latest version)
-    irm https://raw.githubusercontent.com/nghyane/llm-mux/main/install.ps1 | iex
-
-    # Install specific version
-    & { $Version = "v1.0.0"; irm .../install.ps1 | iex }
+.EXAMPLE
+    # Default install (pinned v2.1.3)
+    irm https://raw.githubusercontent.com/nghyane/llm-mux/main/install.ps1 | iex
+
+    # Install specific version (recommended way with parameters)
+    $sb = [ScriptBlock]::Create((irm https://raw.githubusercontent.com/nghyane/llm-mux/main/install.ps1))
+    & $sb -Version "v2.1.3"
@@
 param(
-    [string]$Version = "",
+    # Pinned default version
+    [string]$Version = "v2.1.3",
     [string]$InstallPath = "",
     [switch]$NoService = $false,
     [switch]$NoVerify = $false,
     [switch]$Force = $false
 )
@@
 function Test-Checksum {
@@
-    $checksumContent = Get-Content $ChecksumsPath -Raw
-    $pattern = "([a-fA-F0-9]{64})\s+\*?$([regex]::Escape($FileName))"
+    $checksumContent = Get-Content $ChecksumsPath -Raw
+    $escapedName = [regex]::Escape($FileName)
+    # Format typically: "<sha256>  <filename>" or "<sha256> *<filename>"
+    $pattern = "([a-fA-F0-9]{64})\s+\*?$escapedName"
