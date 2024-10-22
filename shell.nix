{ pkgs ? import <nixpkgs> { } }:

with pkgs;

let
  # TODO: drop after https://github.com/NixOS/nixpkgs/pull/345260 got merged
  postgresql_17 = (import (pkgs.path + /pkgs/servers/sql/postgresql/generic.nix) {
    version = "17.0";
    hash = "sha256-fidhMcD91rYliNutmzuyS4w0mNUAkyjbpZrxboGRCd4=";
  } { self = pkgs; jitSupport = false; }).overrideAttrs ({ nativeBuildInputs, configureFlags , ... }: {
    nativeBuildInputs = nativeBuildInputs ++ (with pkgs; [ bison flex perl docbook_xml_dtd_45 docbook-xsl-nons libxslt ]);
    configureFlags = configureFlags ++ [ "--without-perl" ];
  });
in

mkShell {
  nativeBuildInputs = [
    go-licence-detector
    go_1_23
    golangci-lint
    gotools # goimports
    openssl
    postgresql_17

    # keep this line if you use bash
    bashInteractive
  ];
}
