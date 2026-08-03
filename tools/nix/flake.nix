{
  description = "swissdatasciencecenter/llm-serving development environment";

  nixConfig = {
    extra-substituters = [
      # Nix community's cache server
      "https://nix-community.cachix.org"
      "https://devenv.cachix.org"
    ];
    extra-trusted-public-keys = [
      "nix-community.cachix.org-1:mB9FSh9qf2dCimDSUo8Zy7bkq5CX+/rkCWyvRCYg3Fs="
      "devenv.cachix.org-1:w1cLUi8dv3hnoSPGAuibQv+f9TZLr6cv/Hm9XgU50cw="
    ];
  };

  inputs = {

    # You can access packages and modules from different nixpkgs revs at the same time.
    nixpkgs.url = "github:cachix/devenv-nixpkgs/rolling";
    #nixpkgsStable.url = "github:nixos/nixpkgs/nixos-26.05";

    flake-utils.url = "github:numtide/flake-utils";

    devenv = {
      url = "github:cachix/devenv";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    helmfmt-src = {
      url = "github:digitalstudium/helmfmt/v0.5.0";
      flake = false;
    };
  };

  outputs =
    {
      nixpkgs,
      flake-utils,
      devenv,
      ...
    }@inputs:
    let
      # The function which builds the flake output attrMap.
      defineOutput =
        system:
        let
          # inherit from nixpkgs
          pkgs = nixpkgs.legacyPackages.${system};

          baseTools = with pkgs; [
            bash
            coreutils
            curl
            fd
            findutils
            git
            jq
            just
            kubectl
            kubernetes-helm
            # Formatters driven by ./treefmt.toml (gofmt ships with the go toolchain).
            treefmt
            nixfmt
            prettier
            shfmt
            shellcheck
            (callPackage ./packages/helmfmt.nix { helmfmt-src = inputs.helmfmt-src; })
          ];
          devTools = with pkgs; [
            gitleaks
            prek
            zsh
          ];
          goModule = import ./modules/go.nix { inherit pkgs; };
        in
        {
          devShells = {
            default = devenv.lib.mkShell {
              inherit pkgs inputs;
              modules = goModule ++ [
                { packages = baseTools; }
                { packages = devTools; }
                { enterShell = "just setup"; }
              ];
            };

            ci = devenv.lib.mkShell {
              inherit pkgs inputs;
              modules = goModule ++ [ { packages = baseTools; } ];
            };

          };
        };
    in
    flake-utils.lib.eachDefaultSystem defineOutput;
}
