# Native libraries

Drop the prebuilt **libXray** release AAR here (from https://github.com/XTLS/libXray
releases), e.g. `LibXray.aar`. It is picked up automatically by:

    implementation fileTree(include: ['*.aar'], dir: 'libs')

in `app/build.gradle`. The exact filename does not matter.

If the AAR is absent the app still builds and runs — it simply never leaves
DIRECT mode (LibXrayBridge logs "libXray AAR not found").

Optional: place `geoip.dat` / `geosite.dat` in `app/src/main/assets/` if you add
geo-routing rules to the configs. The bundled endpoints use none, so they are
not required.
