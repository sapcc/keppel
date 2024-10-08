{ pkgs ? import <nixpkgs> { } }:

with pkgs;

let
  # TODO: drop after https://github.com/NixOS/nixpkgs/pull/347304 got merged
  go-licence-detector = buildGoModule rec {
    pname = "go-licence-detector";
    version = "0.7.0";

    src = fetchFromGitHub {
      owner = "elastic";
      repo = "go-licence-detector";
      rev = "v${version}";
      hash = "sha256-43MyzEF7BZ7pcgzDvXx9SjXGHaLozmWkGWUO/yf6K98=";
    };

    vendorHash = "sha256-7vIP5pGFH6CbW/cJp+DiRg2jFcLFEBl8dQzUw1ogTTA=";

    meta = with lib; {
      description = "Detect licences in Go projects and generate documentation";
      homepage = "https://github.com/elastic/go-licence-detector";
      license = licenses.asl20;
      maintainers = with maintainers; [ SuperSandro2000 ];
    };
  };

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
