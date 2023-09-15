function __onGCastApiAvailable(isAvailable) {
    console.log('Loading cast framework...', isAvailable);
    if (!isAvailable) {
        return;
    }
    cast.framework.CastContext.getInstance().setOptions({
        autoJoinPolicy: chrome.cast.AutoJoinPolicy.ORIGIN_SCOPED,
        receiverApplicationId: chrome.cast.media.DEFAULT_MEDIA_RECEIVER_APP_ID,
    });
}
