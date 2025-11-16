#!/bin/sh
set -e

RAM=${PAPER_RAM:-1G}

sed -i "s/^ *secret:.*/    secret: \"$VELOCITY_SECRET\"/" paper/config/paper-global.yml
sed -i "s/^ *velocitySecret:.*/    velocitySecret: \"$VELOCITY_SECRET\"/" gate/config.yml
echo "eula=true" > paper/eula.txt

cd gate
./gate -c config.yml &
cd ..

if [ -z "$PAPER_VERSION" ]; then
  PAPER_VERSION=$(curl -s https://api.papermc.io/v2/projects/paper | jq -r '.versions[]' | sort -V | tail -n 1)
fi

latest_build=$(curl -s "https://api.papermc.io/v2/projects/paper/versions/$PAPER_VERSION/builds" | jq -r '.builds[-1].build')
curl -sSL "https://api.papermc.io/v2/projects/paper/versions/$PAPER_VERSION/builds/$latest_build/downloads/paper-$PAPER_VERSION-$latest_build.jar" -o paper/paper.jar

cd paper
java -Xmx"$RAM" -Xms"$RAM" -jar paper.jar nogui