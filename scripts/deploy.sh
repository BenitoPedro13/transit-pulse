#!/bin/bash
# Mac side of `make deploy` (docs/tasks/TASK-aws-infra.md §2.4): build the arm64 image, push it
# to ECR tagged with the git SHA, upload the release files to the ops bucket and run
# remote-deploy.sh on the box through SSM. Refuses to run on a dirty tree.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ -n "$(git status --porcelain)" ]; then
  echo "refusing to deploy: the git tree is dirty (commit first, so the image tag means something)" >&2
  exit 1
fi

REGION=us-east-1
SHA="$(git rev-parse --short=12 HEAD)"
ACCOUNT="$(aws sts get-caller-identity --query Account --output text)"
ECR="${ACCOUNT}.dkr.ecr.${REGION}.amazonaws.com"
OPS="transit-pulse-ops-${ACCOUNT}"
IMAGE="${ECR}/transit-pulse/go:${SHA}"
INSTANCE="$(aws ec2 describe-instances --region "${REGION}" \
  --filters Name=tag:Name,Values=transit-pulse-app Name=instance-state-name,Values=running \
  --query 'Reservations[0].Instances[0].InstanceId' --output text)"
[ "${INSTANCE}" != "None" ] || { echo "no running transit-pulse-app instance" >&2; exit 1; }

aws ecr get-login-password --region "${REGION}" | docker login --username AWS --password-stdin "${ECR}" >/dev/null
if aws ecr describe-images --region "${REGION}" --repository-name transit-pulse/go --image-ids imageTag="${SHA}" >/dev/null 2>&1; then
  echo "image ${SHA} already in ECR (tags are immutable); skipping build"
else
  docker build --platform linux/arm64 -t "${IMAGE}" .
  docker push "${IMAGE}"
fi

aws s3 sync infra/aws "s3://${OPS}/deploy/${SHA}/infra/aws/" --delete --only-show-errors
aws s3 sync infra/postgres/initdb "s3://${OPS}/deploy/${SHA}/infra/postgres/initdb/" --delete --only-show-errors

CMD_ID="$(aws ssm send-command --region "${REGION}" --instance-ids "${INSTANCE}" \
  --document-name AWS-RunShellScript --timeout-seconds 600 \
  --parameters "commands=[\"aws s3 cp s3://${OPS}/deploy/${SHA}/infra/aws/remote-deploy.sh /opt/transit-pulse/remote-deploy.sh --region ${REGION} && chmod +x /opt/transit-pulse/remote-deploy.sh && /opt/transit-pulse/remote-deploy.sh ${SHA}\"]" \
  --query Command.CommandId --output text)"
echo "SSM command ${CMD_ID} on ${INSTANCE}, release ${SHA}"

while :; do
  STATUS="$(aws ssm get-command-invocation --region "${REGION}" --command-id "${CMD_ID}" --instance-id "${INSTANCE}" --query Status --output text 2>/dev/null || echo Pending)"
  case "${STATUS}" in Pending|InProgress|Delayed) sleep 5 ;; *) break ;; esac
done
aws ssm get-command-invocation --region "${REGION}" --command-id "${CMD_ID}" --instance-id "${INSTANCE}" \
  --query '[StandardOutputContent,StandardErrorContent]' --output text
[ "${STATUS}" = "Success" ] || { echo "deploy ${STATUS}" >&2; exit 1; }
echo "deployed ${SHA}"
