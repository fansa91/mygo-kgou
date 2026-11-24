package dev.mygo

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.DocumentsContract
import org.json.JSONObject
import java.io.File

/**
 * A folder of music the app reads where it is: the picker names a folder of
 * the phone's storage, whose path the app keeps, and the songs are read with
 * the audio permission the app asks for, never copied.
 */
class LocalFolderPlugin : MyGoPlugin {
    private var picking: PluginReply? = null

    private val prefs get() = PluginHost.context.getSharedPreferences("localfolder", 0)

    /** The permission a folder of songs is read with: the audio files' on
     *  Android 13 and later, the whole of shared storage before that. */
    private fun mediaPermission(): String =
        if (Build.VERSION.SDK_INT >= 33) Manifest.permission.READ_MEDIA_AUDIO
        else Manifest.permission.READ_EXTERNAL_STORAGE

    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        when (method) {
            "pick" -> PluginHost.requestPermissions(arrayOf(mediaPermission())) { granted ->
                if (!granted) {
                    reply.failure(PluginFailure("Denied", "the app may not read the songs on the phone"))
                    return@requestPermissions
                }
                picking = reply
                val intent = Intent(Intent.ACTION_OPEN_DOCUMENT_TREE).apply {
                    addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION)
                }
                PluginHost.startForResult(intent) { result, data ->
                    val r = picking
                    picking = null
                    if (r == null) return@startForResult
                    val uri = data?.data
                    if (result != Activity.RESULT_OK || uri == null) {
                        r.success(JSONObject().put("path", "").put("saved", false))
                        return@startForResult
                    }
                    try {
                        PluginHost.context.contentResolver
                            .takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION)
                    } catch (_: Exception) {
                        // The grant is for this run only: the path is kept, so
                        // the songs are still there next time, permission
                        // being what the media permission covers.
                    }
                    val path = pathOf(uri)
                    if (path == null) {
                        r.failure(PluginFailure("NoPath", "the app can read only the phone's own storage"))
                        return@startForResult
                    }
                    prefs.edit().putString("path", path).apply()
                    r.success(JSONObject().put("path", path).put("saved", true))
                }
            }
            "restore" -> {
                val path = prefs.getString("path", "") ?: ""
                if (path.isEmpty()) {
                    reply.success(JSONObject().put("path", "").put("saved", false))
                    return
                }
                // A folder that is gone, or that the system no longer lets the
                // app read, is no folder to go back to: the user picks again.
                if (!readable(path)) {
                    prefs.edit().remove("path").apply()
                    reply.success(JSONObject().put("path", "").put("saved", true))
                    return
                }
                reply.success(JSONObject().put("path", path).put("saved", true))
            }
            "forget" -> {
                prefs.edit().remove("path").apply()
                reply.success(JSONObject().put("path", "").put("saved", false))
            }
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }

    private fun readable(path: String): Boolean = try {
        val dir = File(path)
        dir.isDirectory && dir.list() != null
    } catch (_: Exception) {
        false
    }

    /**
     * The path a picked tree names. The system's document ids start with the
     * volume, as in "primary:Music", which names a folder of the storage the
     * phone shows to its apps.
     */
    private fun pathOf(uri: Uri): String? {
        val id = try {
            DocumentsContract.getTreeDocumentId(uri)
        } catch (_: Exception) {
            return null
        }
        val colon = id.indexOf(':')
        val volume = if (colon < 0) id else id.substring(0, colon)
        val rel = if (colon < 0) "" else id.substring(colon + 1)
        val root = when (volume) {
            "primary" -> Environment.getExternalStorageDirectory().absolutePath
            "home" -> Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOCUMENTS).absolutePath
            else -> "/storage/$volume"
        }
        return if (rel.isEmpty()) root else File(root, rel).absolutePath
    }
}
