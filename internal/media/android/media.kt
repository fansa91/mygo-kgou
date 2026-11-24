// What the system shows of what the app plays, and the commands of its media
// controls: the lock screen, the notification shade, the headphones' buttons
// and the car's. Go sets the track with package media, which this code tells
// back what the controls ask for, through a MediaSession and the media
// notification the system takes its controls from.

package dev.mygo

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.drawable.Icon
import android.media.MediaMetadata
import android.media.session.MediaSession
import android.media.session.PlaybackState
import android.os.Build
import android.util.Base64
import org.json.JSONObject

class MediaPlugin : MyGoPlugin {
    companion object {
        private const val ID = "media"
        private const val CHANNEL = "playback"
        private const val NOTE = 0x4d45
        private const val EXTRA = "command"
    }

    private var session: MediaSession? = null
    private var title = ""
    private var artist = ""
    private var duration = 0L
    private var position = 0L
    private var playing = false
    private var artwork: Bitmap? = null
    // asked says the user was asked for the notification, which the controls
    // come with on Android 13 and later.
    private var asked = false

    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        when (method) {
            "set" -> {
                apply(args)
                reply.success(null)
            }
            "clear" -> {
                clear()
                reply.success(null)
            }
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }

    /** Shows what Go says plays, and offers the commands it answers. */
    private fun apply(a: JSONObject) {
        val s = ensureSession()
        title = a.optString("title")
        artist = a.optString("artist")
        duration = a.optLong("durationMs")
        position = a.optLong("positionMs")
        playing = a.optBoolean("playing")
        // The cover comes only when it changed: the same one shows again.
        if (a.has("artwork")) {
            val b64 = a.optString("artwork")
            artwork = if (b64.isEmpty()) null else decode(b64)
        }

        val meta = MediaMetadata.Builder()
            .putString(MediaMetadata.METADATA_KEY_TITLE, title)
            .putString(MediaMetadata.METADATA_KEY_ARTIST, artist)
            .putString(MediaMetadata.METADATA_KEY_ALBUM, a.optString("album"))
            .putLong(MediaMetadata.METADATA_KEY_DURATION, duration)
        artwork?.let { meta.putBitmap(MediaMetadata.METADATA_KEY_ALBUM_ART, it) }
        s.setMetadata(meta.build())

        var actions = (PlaybackState.ACTION_PLAY or PlaybackState.ACTION_PAUSE or
            PlaybackState.ACTION_PLAY_PAUSE or PlaybackState.ACTION_STOP)
        if (a.optBoolean("canNext")) actions = actions or PlaybackState.ACTION_SKIP_TO_NEXT
        if (a.optBoolean("canPrev")) actions = actions or PlaybackState.ACTION_SKIP_TO_PREVIOUS
        if (a.optBoolean("canSeek")) actions = actions or PlaybackState.ACTION_SEEK_TO
        s.setPlaybackState(
            PlaybackState.Builder()
                .setActions(actions)
                .setState(
                    if (playing) PlaybackState.STATE_PLAYING else PlaybackState.STATE_PAUSED,
                    position,
                    if (playing) 1f else 0f
                )
                .build()
        )

        show(a.optBoolean("canNext"), a.optBoolean("canPrev"))
    }

    /** The session the system's controls drive, which starts one. */
    private fun ensureSession(): MediaSession {
        session?.let { return it }
        val s = MediaSession(PluginHost.context, "mygo-media")
        s.setCallback(object : MediaSession.Callback() {
            override fun onPlay() = press("play")
            override fun onPause() = press("pause")
            override fun onStop() = press("stop")
            override fun onSkipToNext() = press("next")
            override fun onSkipToPrevious() = press("prev")

            override fun onSeekTo(pos: Long) {
                PluginHost.emit(ID, "command", mapOf("command" to "seek", "positionMs" to pos))
            }
        })
        s.isActive = true
        session = s
        return s
    }

    /** Takes the app out of the system's controls. */
    private fun clear() {
        playing = false
        artwork = null
        session?.isActive = false
        session?.release()
        session = null
        val mgr = PluginHost.context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        mgr.cancel(NOTE)
    }

    /** Posts the media notification, which the system shows the controls from. */
    private fun show(canNext: Boolean, canPrev: Boolean) {
        val ctx = PluginHost.context
        if (Build.VERSION.SDK_INT >= 33 &&
            ctx.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            if (!asked) {
                asked = true
                PluginHost.requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS)) { granted ->
                    if (granted) show(canNext, canPrev)
                }
            }
            return
        }
        val mgr = ctx.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val channel = NotificationChannel(CHANNEL, "播放控制", NotificationManager.IMPORTANCE_LOW)
        channel.setShowBadge(false)
        mgr.createNotificationChannel(channel)

        val b = Notification.Builder(ctx, CHANNEL)
            .setSmallIcon(if (ctx.applicationInfo.icon != 0) ctx.applicationInfo.icon else android.R.drawable.ic_media_play)
            .setContentTitle(title)
            .setContentText(artist)
            .setContentIntent(openApp(ctx))
            .setVisibility(Notification.VISIBILITY_PUBLIC)
            .setCategory(Notification.CATEGORY_TRANSPORT)
            .setOngoing(playing)
            .setShowWhen(false)
            .setOnlyAlertOnce(true)
        artwork?.let { b.setLargeIcon(it) }

        val actions = ArrayList<Notification.Action>(3)
        if (canPrev) actions.add(action(ctx, android.R.drawable.ic_media_previous, "上一首", "prev"))
        actions.add(
            if (playing) action(ctx, android.R.drawable.ic_media_pause, "暂停", "toggle")
            else action(ctx, android.R.drawable.ic_media_play, "播放", "toggle")
        )
        if (canNext) actions.add(action(ctx, android.R.drawable.ic_media_next, "下一首", "next"))
        for (a in actions) b.addAction(a)

        // The buttons the shrunk notification shows are the first three.
        val compact = minOf(actions.size, 3)
        b.setStyle(
            Notification.MediaStyle()
                .setMediaSession(session?.sessionToken)
                .setShowActionsInCompactView(*Array(compact) { it })
        )
        mgr.notify(NOTE, b.build())
    }

    private fun action(ctx: Context, icon: Int, label: String, command: String): Notification.Action {
        val i = Intent(ctx, MediaControlReceiver::class.java).putExtra(EXTRA, command)
        val pending = PendingIntent.getBroadcast(
            ctx, command.hashCode(), i,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        return Notification.Action.Builder(Icon.createWithResource(ctx, icon), label, pending).build()
    }

    /** Opens the app, as tapping the notification does. */
    private fun openApp(ctx: Context): PendingIntent? {
        val i = ctx.packageManager.getLaunchIntentForPackage(ctx.packageName) ?: return null
        i.addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP)
        return PendingIntent.getActivity(
            ctx, 0, i,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
    }

    private fun decode(b64: String): Bitmap? = runCatching {
        val bytes = Base64.decode(b64, Base64.DEFAULT)
        BitmapFactory.decodeByteArray(bytes, 0, bytes.size)
    }.getOrNull()

    private fun press(command: String) {
        PluginHost.emit(ID, "command", mapOf("command" to command))
    }
}

/** The buttons of the media notification, which send what they press. */
class MediaControlReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val command = intent.getStringExtra("command") ?: return
        PluginHost.emit("media", "command", mapOf("command" to command))
    }
}
