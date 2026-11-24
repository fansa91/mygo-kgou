package dev.mygo

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.DocumentsContract
import org.json.JSONArray
import org.json.JSONObject
import java.io.File

/**
 * A folder of music the app reads where it is: the picker names a folder of
 * the phone's storage, and the app keeps the system's permission to that
 * document tree. Android 11 and later do not let an app list a shared folder
 * by path, so the Go side asks this plugin for the files with "list"; the
 * files themselves are opened by path, which the audio permission allows.
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
                        // A provider may refuse to keep the grant; the folder
                        // still works for this run, and restore asks again.
                    }
                    val path = pathOf(uri)
                    if (path == null) {
                        r.failure(PluginFailure("NoPath", "the app can read only the phone's own storage"))
                        return@startForResult
                    }
                    prefs.edit().putString("path", path).putString("uri", uri.toString()).apply()
                    r.success(JSONObject().put("path", path).put("saved", true))
                }
            }
            "restore" -> {
                val path = prefs.getString("path", "") ?: ""
                val tree = prefs.getString("uri", "") ?: ""
                if (path.isEmpty()) {
                    reply.success(JSONObject().put("path", "").put("saved", false))
                    return
                }
                // A folder picked by an earlier build has no tree saved: it
                // has to be picked again, since its path alone cannot be
                // listed. The app's own folder has no tree and is ours.
                val gone = if (tree.isEmpty()) !readable(path) else !granted(tree) && !readable(path)
                if (gone) {
                    prefs.edit().remove("path").remove("uri").apply()
                    reply.success(JSONObject().put("path", "").put("saved", true))
                } else {
                    reply.success(JSONObject().put("path", path).put("saved", true))
                }
            }
            "list" -> {
                val tree = prefs.getString("uri", "") ?: ""
                val paths = JSONArray()
                if (tree.isNotEmpty() && granted(tree)) {
                    try {
                        val uri = Uri.parse(tree)
                        walk(uri, DocumentsContract.getTreeDocumentId(uri), paths, 0)
                    } catch (_: Exception) {
                        // A permission the system took back: no files to read.
                    }
                }
                reply.success(JSONObject().put("paths", paths))
            }
            "forget" -> {
                prefs.edit().remove("path").remove("uri").apply()
                reply.success(JSONObject().put("path", "").put("saved", false))
            }
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }

    /** Whether the system's lasting permission to this tree is still held. */
    private fun granted(tree: String): Boolean = try {
        val uri = Uri.parse(tree)
        PluginHost.context.contentResolver.persistedUriPermissions.any {
            it.uri == uri && it.isReadPermission
        }
    } catch (_: Exception) {
        false
    }

    /** A directory that is ours to read without the document tree, as the
     *  app's own folder is. */
    private fun readable(path: String): Boolean = try {
        val dir = File(path)
        dir.isDirectory && dir.list() != null
    } catch (_: Exception) {
        false
    }

    /** The files under one folder of the tree, and under its folders too.
     *  Java's File cannot list shared storage on Android 11+, but the tree
     *  the user picked can be listed with DocumentsContract. */
    private fun walk(tree: Uri, id: String, out: JSONArray, depth: Int) {
        if (depth > 32) return
        val children = DocumentsContract.buildChildDocumentsUriUsingTree(tree, id)
        val columns = arrayOf(
            DocumentsContract.Document.COLUMN_DOCUMENT_ID,
            DocumentsContract.Document.COLUMN_DISPLAY_NAME,
            DocumentsContract.Document.COLUMN_MIME_TYPE,
        )
        val cursor = PluginHost.context.contentResolver.query(children, columns, null, null, null) ?: return
        cursor.use { c ->
            val idAt = c.getColumnIndex(columns[0])
            val nameAt = c.getColumnIndex(columns[1])
            val mimeAt = c.getColumnIndex(columns[2])
            while (c.moveToNext()) {
                val child = if (idAt >= 0) c.getString(idAt) ?: "" else ""
                if (child.isEmpty()) continue
                val mime = if (mimeAt >= 0) c.getString(mimeAt) ?: "" else ""
                val name = if (nameAt >= 0) c.getString(nameAt) ?: "" else ""
                if (mime == DocumentsContract.Document.MIME_TYPE_DIR) {
                    if (!name.startsWith(".")) walk(tree, child, out, depth + 1)
                    continue
                }
                if (name.startsWith(".")) continue
                pathOfDocumentId(child)?.let { out.put(it) }
            }
        }
    }

    /** The path a picked tree or document id names. The system's ids start
     *  with the volume, as in "primary:Music", which is a folder of the
     *  storage the phone shows to its apps. */
    private fun pathOf(uri: Uri): String? = try {
        pathOfDocumentId(DocumentsContract.getTreeDocumentId(uri))
    } catch (_: Exception) {
        null
    }

    private fun pathOfDocumentId(id: String): String? {
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
