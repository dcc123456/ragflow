
# 谷歌云迁移事宜说明

## login

gcloud init  --no-launch-browser

## create snapshots

gcloud compute snapshots create google04 \
    --source-disk ragflow04 \
    --source-disk-zone us-west1-a

## create image

gcloud compute images create image04 \
    --source-snapshot=google04

## share image

gcloud compute images add-iam-policy-binding image04 \
    --member='allAuthenticatedUsers' \
    --role='roles/compute.imageUser'

## create VM via image

gcloud compute instances create ragflow02 \
    --image-project ragflow-01 \
    --image  image02
