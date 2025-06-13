Some small utilities that I use to create my personal workflows in Wayland.
Screenshot, kitty panel based bar, backlight control, various minor compositor integration
endpoints, etc. Not really re-useable by anyone else, but perhaps the code can
serve as an inspiration.

To build::

    git checkout https://github.com/kovidgoyal/wm.git && cd wm && go build .

To run::

    ./wm --help
