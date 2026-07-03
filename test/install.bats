#!/usr/bin/env bats
# Logic tests for install.sh, run in CI (ubuntu). The script is sourced with
# ARRSENAL_SOURCED=1 (main never runs) and its probe files pointed at
# fixtures — the REAL functions are under test.

make_os_release() { # id version_id → path
  local f="$BATS_TEST_TMPDIR/os-release-$1-$2"
  printf 'ID=%s\nVERSION_ID="%s"\n' "$1" "$2" > "$f"
  echo "$f"
}

tier_for() { # id version_id → tier
  ARRSENAL_SOURCED=1 ARRSENAL_OS_RELEASE="$(make_os_release "$1" "$2")" \
    bash -c "source '$BATS_TEST_DIRNAME/../install.sh'; distro_tier"
}

@test "detect_arch maps machine names" {
  src="source '$BATS_TEST_DIRNAME/../install.sh'"
  run env ARRSENAL_SOURCED=1 bash -c "$src; uname() { echo x86_64; }; detect_arch"
  [ "$status" -eq 0 ] && [ "$output" = "amd64" ]
  run env ARRSENAL_SOURCED=1 bash -c "$src; uname() { echo aarch64; }; detect_arch"
  [ "$status" -eq 0 ] && [ "$output" = "arm64" ]
  run env ARRSENAL_SOURCED=1 bash -c "$src; uname() { echo riscv64; }; detect_arch"
  [ "$status" -ne 0 ]
}

@test "distro tiers: debian/ubuntu split by version, RPM family coverable, unknown manual" {
  [ "$(tier_for debian 12)" = "tier1" ]
  [ "$(tier_for debian 13)" = "tier1" ]
  [ "$(tier_for debian 11)" = "coverable" ]
  [ "$(tier_for ubuntu 24.04)" = "tier1" ]
  [ "$(tier_for ubuntu 22.04)" = "tier1" ]
  [ "$(tier_for ubuntu 20.04)" = "coverable" ]
  [ "$(tier_for fedora 42)" = "coverable" ]
  [ "$(tier_for rocky 9)" = "coverable" ]
  [ "$(tier_for arch '')" = "manual" ]
  [ "$(tier_for gentoo 2.17)" = "manual" ]
}

@test "missing os-release is manual, never a crash" {
  run env ARRSENAL_SOURCED=1 ARRSENAL_OS_RELEASE=/nonexistent \
    bash -c "source '$BATS_TEST_DIRNAME/../install.sh'; distro_tier"
  [ "$status" -eq 0 ] && [ "$output" = "manual" ]
}

@test "is_wsl fires on a microsoft kernel string only" {
  echo "Linux version 6.6.36.6-microsoft-standard-WSL2" > "$BATS_TEST_TMPDIR/wsl"
  echo "Linux version 6.8.0-45-generic (buildd@lcy02)" > "$BATS_TEST_TMPDIR/real"
  run env ARRSENAL_SOURCED=1 ARRSENAL_PROC_VERSION="$BATS_TEST_TMPDIR/wsl" \
    bash -c "source '$BATS_TEST_DIRNAME/../install.sh'; is_wsl"
  [ "$status" -eq 0 ]
  run env ARRSENAL_SOURCED=1 ARRSENAL_PROC_VERSION="$BATS_TEST_TMPDIR/real" \
    bash -c "source '$BATS_TEST_DIRNAME/../install.sh'; is_wsl"
  [ "$status" -ne 0 ]
}

@test "is_lxc fires on container=lxc in pid1 environ" {
  printf 'container=lxc\0PATH=/usr/bin\0' > "$BATS_TEST_TMPDIR/lxc"
  printf 'PATH=/usr/bin\0' > "$BATS_TEST_TMPDIR/vm"
  run env ARRSENAL_SOURCED=1 ARRSENAL_PID1_ENVIRON="$BATS_TEST_TMPDIR/lxc" \
    bash -c "source '$BATS_TEST_DIRNAME/../install.sh'; is_lxc"
  [ "$status" -eq 0 ]
  run env ARRSENAL_SOURCED=1 ARRSENAL_PID1_ENVIRON="$BATS_TEST_TMPDIR/vm" \
    bash -c "source '$BATS_TEST_DIRNAME/../install.sh'; is_lxc"
  [ "$status" -ne 0 ]
}

@test "ask: ARRSENAL_YES accepts default-yes and still refuses default-no" {
  src="source '$BATS_TEST_DIRNAME/../install.sh'"
  run env ARRSENAL_SOURCED=1 ARRSENAL_YES=1 bash -c "$src; ask 'install?' y"
  [ "$status" -eq 0 ]
  run env ARRSENAL_SOURCED=1 ARRSENAL_YES=1 bash -c "$src; ask 'best effort?' n"
  [ "$status" -ne 0 ]
}

@test "sourcing guard: main never runs when sourced" {
  run env ARRSENAL_SOURCED=1 bash -c "source '$BATS_TEST_DIRNAME/../install.sh'; echo sourced-ok"
  [ "$status" -eq 0 ]
  [[ "$output" == *"sourced-ok"* ]]
  [[ "$output" != *"bootstrap"* ]]
}
