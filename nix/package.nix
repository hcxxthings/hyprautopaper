{
  lib,
  buildGoModule,
  installShellFiles,
  libnotify,
}:

buildGoModule (finalAttrs: {
  pname = "hyprautopaper";
  version = "0.1.0";

  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      (lib.fileset.fileFilter (file: file.hasExt "go") ../.)
    ];
  };

  # Standard library only.
  vendorHash = null;

  ldflags = [
    "-s"
    "-w"
    "-X main.version=${finalAttrs.version}"
    # Notifications work without libnotify in the user's profile.
    "-X main.notifySend=${lib.getExe' libnotify "notify-send"}"
  ];

  nativeBuildInputs = [ installShellFiles ];

  postInstall = ''
    installShellCompletion --cmd hyprautopaper \
      --bash <($out/bin/hyprautopaper completion bash) \
      --zsh <($out/bin/hyprautopaper completion zsh) \
      --fish <($out/bin/hyprautopaper completion fish)
  '';

  meta = {
    description = "Wallpaper changer with Windows Spotlight, Bing and wallhaven images, for hyprpaper and other setters";
    homepage = "https://github.com/hcxxthings/hyprautopaper";
    license = lib.licenses.mit;
    mainProgram = "hyprautopaper";
    platforms = lib.platforms.linux;
  };
})
