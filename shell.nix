{ pkgs ? import <nixpkgs> { } }:

with pkgs;

mkShell {
  nativeBuildInputs = [
    go-licence-detector
    go_1_23
    golangci-lint
    gotools # goimports
    kubernetes-controller-tools # controller-gen
    setup-envtest

    # keep this line if you use bash
    bashInteractive
  ];
}