#!/bin/bash
set -e

if [ -z "$VELOCITY_SECRET" ]; then
  echo "VELOCITY_SECRET not set" >&2
  exit 1
fi

# Velocity-Secret in Paper- und Gate-Config setzen
# Erwartet in paper/config/paper-global.yml:
#   secret: "dummy"
sed -i "s/^ *secret:.*/    secret: \"$VELOCITY_SECRET\"/" paper/config/paper-global.yml

# Erwartet in gate/config.yml:
#   velocitySecret: "dummy"
sed -i "s/^ *velocitySecret:.*/    velocitySecret: \"$VELOCITY_SECRET\"/" gate/config.yml

echo "eula=true" > paper/eula.txt

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

cd paper
java -Xmx2G -jar paper.jar nogui &
sleep 5

cd ../gate
./gate -c config.yml
