import Foundation
import UIKit
import UniformTypeIdentifiers

/// A folder of music the app reads where it is: the picker hands over the
/// folder itself, and its permission is kept in a bookmark the next run
/// resolves, so the songs are played from the folder instead of being copied.
final class LocalFolderPlugin: NSObject, MyGoPlugin, UIDocumentPickerDelegate {
    /// The answer waiting for the picker to close.
    private var picking: PluginReply?
    /// The folder whose permission this run holds.
    private var open: URL?

    private static let bookmarkKey = "localfolder.bookmark"
    private static let pathKey = "localfolder.path"

    func call(_ method: String, _ args: [String: Any], _ reply: @escaping PluginReply) {
        switch method {
        case "pick":
            picking = reply
            let p = UIDocumentPickerViewController(forOpeningContentTypes: [.folder], asCopy: false)
            p.allowsMultipleSelection = false
            p.delegate = self
            PluginHost.present(p)
        case "restore":
            let saved = restore()
            reply(.success(["path": saved.path, "saved": saved.picked]))
        case "forget":
            forget()
            reply(.success(["path": "", "saved": false]))
        default:
            reply(.failure(PluginFailure("NoMethod", method)))
        }
    }

    func documentPicker(_ p: UIDocumentPickerViewController, didPickDocumentsAt urls: [URL]) {
        let reply = picking
        picking = nil
        guard let url = urls.first else {
            reply?(.success(["path": "", "saved": false]))
            return
        }
        keep(url)
        reply?(.success(["path": url.path, "saved": true]))
    }

    func documentPickerWasCancelled(_ p: UIDocumentPickerViewController) {
        let reply = picking
        picking = nil
        reply?(.success(["path": "", "saved": false]))
    }

    /// Keeps the permission to a folder the picker handed over: this run holds
    /// it, and a bookmark hands it to the next.
    private func keep(_ url: URL) {
        open?.stopAccessingSecurityScopedResource()
        _ = url.startAccessingSecurityScopedResource()
        open = url
        let d = UserDefaults.standard
        if let data = try? url.bookmarkData(options: [], includingResourceValuesForKeys: nil, relativeTo: nil) {
            d.set(data, forKey: Self.bookmarkKey)
        }
        d.set(url.path, forKey: Self.pathKey)
    }

    private func forget() {
        open?.stopAccessingSecurityScopedResource()
        open = nil
        let d = UserDefaults.standard
        d.removeObject(forKey: Self.bookmarkKey)
        d.removeObject(forKey: Self.pathKey)
    }

    /// Takes the permission back, and says whether the folder is still there
    /// to read.
    private func restore() -> (path: String, picked: Bool) {
        let d = UserDefaults.standard
        let picked = d.data(forKey: Self.bookmarkKey) != nil || d.string(forKey: Self.pathKey) != nil
        var url: URL?
        if let data = d.data(forKey: Self.bookmarkKey) {
            var stale = false
            url = try? URL(resolvingBookmarkData: data, options: [], relativeTo: nil, bookmarkDataIsStale: &stale)
        }
        if url == nil, let p = d.string(forKey: Self.pathKey) {
            url = URL(fileURLWithPath: p)
        }
        guard let u = url else { return ("", picked) }
        _ = u.startAccessingSecurityScopedResource()
        open = u
        // A folder that is gone, or that the system no longer lets the app
        // read, is no folder to go back to: the user picks it again.
        guard (try? FileManager.default.contentsOfDirectory(atPath: u.path)) != nil else {
            forget()
            return ("", picked)
        }
        return (u.path, true)
    }
}
