#!/bin/sh
set -e

if [ -z "$VELOCITY_SECRET" ]; then
  echo "VELOCITY_SECRET not set" >&2
  exit 1
fi

sed -i "s/^ *secret:.*/    secret: \"$VELOCITY_SECRET\"/" paper/config/paper-global.yml
sed -i "s/^ *velocitySecret:.*/    velocitySecret: \"$VELOCITY_SECRET\"/" gate/config.yml

if [ -z "$PAPER_VERSION" ]; then
  PAPER_VERSION=$(
    curl -s https://api.papermc.io/v2/projects/paper \
    | jq -r '.versions[]' \
    | sort -V \
    | tail -n 1
  )
fi

latest_build=$(
  curl -s "https://api.papermc.io/v2/projects/paper/versions/$PAPER_VERSION/builds" \
  | jq -r '.builds[-1].build'
)

paper_url="https://api.papermc.io/v2/projects/paper/versions/$PAPER_VERSION/builds/$latest_build/downloads/paper-$PAPER_VERSION-$latest_build.jar"
curl -sSL "$paper_url" -o paper/paper.jar

gate_release=$(curl -s https://api.github.com/repos/minekube/gate/releases/latest)
gate_tag=$(echo "$gate_release" | jq -r '.tag_name')
gate_url=$(
  echo "$gate_release" \
  | jq -r '.assets[] | select(.name | test("linux.*amd64")) | .browser_download_url' \
  | head -n 1
)

if [ -z "$gate_url" ]; then
  gate_url="https://github.com/minekube/gate/releases/download/$gate_tag/gate-linux-amd64"
fi

curl -sSL "$gate_url" -o gate/gate
chmod +x gate/gate

cd paper
java -Xmx2G -jar paper.jar nogui &
sleep 5
cd ../gate
./gate
