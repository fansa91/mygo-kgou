// What the system shows of what the app plays, and the commands of its media
// controls: the lock screen, Control Center, the headphones' buttons and the
// car's. Go sets the track with package media, which this code tells back what
// the controls ask for.

import AVFoundation
import MediaPlayer
import UIKit

final class MediaPlugin: MyGoPlugin {
    /// The cover the system shows: a set without one keeps it, until a track
    /// with none clears it.
    private var artwork: MPMediaItemArtwork?

    init() {
        // Music, not a sound effect: the side switch does not silence it, and
        // it goes on with the app in the background, which its Info.plist's
        // UIBackgroundModes audio allows. The session is activated when
        // something plays, not before.
        try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .default)
        let center = MPRemoteCommandCenter.shared()
        center.playCommand.addTarget { [weak self] _ in
            self?.emit("play")
            return .success
        }
        center.pauseCommand.addTarget { [weak self] _ in
            self?.emit("pause")
            return .success
        }
        center.togglePlayPauseCommand.addTarget { [weak self] _ in
            self?.emit("toggle")
            return .success
        }
        center.nextTrackCommand.addTarget { [weak self] _ in
            self?.emit("next")
            return .success
        }
        center.previousTrackCommand.addTarget { [weak self] _ in
            self?.emit("prev")
            return .success
        }
        center.stopCommand.addTarget { [weak self] _ in
            self?.emit("stop")
            return .success
        }
        center.changePlaybackPositionCommand.addTarget { [weak self] event in
            guard let e = event as? MPChangePlaybackPositionCommandEvent else {
                return .commandFailed
            }
            self?.emit("seek", positionMs: Int64(e.positionTime * 1000))
            return .success
        }
        // Nothing shows until Go says what plays.
        disable()
    }

    func call(_ method: String, _ args: [String: Any], _ reply: @escaping PluginReply) {
        switch method {
        case "set":
            apply(args)
            reply(.success(nil))
        case "clear":
            clear()
            reply(.success(nil))
        default:
            reply(.failure(PluginFailure("NoMethod", method)))
        }
    }

    /// Shows what Go says plays, and offers the commands it answers.
    private func apply(_ a: [String: Any]) {
        let playing = a["playing"] as? Bool ?? false
        let duration = (a["durationMs"] as? Double ?? 0) / 1000
        let position = (a["positionMs"] as? Double ?? 0) / 1000
        var info: [String: Any] = [
            MPMediaItemPropertyTitle: a["title"] as? String ?? "",
            MPMediaItemPropertyArtist: a["artist"] as? String ?? "",
            MPMediaItemPropertyAlbumTitle: a["album"] as? String ?? "",
            MPNowPlayingInfoPropertyElapsedPlaybackTime: position,
            MPNowPlayingInfoPropertyPlaybackRate: playing ? 1.0 : 0.0,
            MPNowPlayingInfoPropertyDefaultPlaybackRate: 1.0,
        ]
        if duration > 0 {
            info[MPMediaItemPropertyPlaybackDuration] = duration
        }
        // The cover comes only when it changed: the same one shows again.
        if let b64 = a["artwork"] as? String {
            if b64.isEmpty {
                artwork = nil
            } else if let data = Data(base64Encoded: b64), let image = UIImage(data: data) {
                artwork = MPMediaItemArtwork(boundsSize: image.size) { _ in image }
            }
        }
        if let art = artwork {
            info[MPMediaItemPropertyArtwork] = art
        }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
        enable(next: a["canNext"] as? Bool ?? false,
               prev: a["canPrev"] as? Bool ?? false,
               seek: a["canSeek"] as? Bool ?? false)
        if playing {
            // A session of the app's own, which goes on in the background.
            try? AVAudioSession.sharedInstance().setActive(true)
        }
    }

    /// Takes the app out of the system's controls.
    private func clear() {
        MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
        artwork = nil
        disable()
        try? AVAudioSession.sharedInstance().setActive(false, options: [.notifyOthersOnDeactivation])
    }

    /// Offers the commands that Go does something with, and no others: a
    /// command the app ignores would only take the headphones' press away
    /// from other apps. Play and pause stay offered while a track shows, so
    /// that a paused one can go on from the lock screen.
    private func enable(next: Bool, prev: Bool, seek: Bool) {
        let center = MPRemoteCommandCenter.shared()
        center.playCommand.isEnabled = true
        center.pauseCommand.isEnabled = true
        center.togglePlayPauseCommand.isEnabled = true
        center.stopCommand.isEnabled = true
        center.nextTrackCommand.isEnabled = next
        center.previousTrackCommand.isEnabled = prev
        center.changePlaybackPositionCommand.isEnabled = seek
    }

    /// Offers nothing: the app shows nothing, and the system's controls and
    /// the headphones' buttons go to other apps.
    private func disable() {
        let center = MPRemoteCommandCenter.shared()
        center.playCommand.isEnabled = false
        center.pauseCommand.isEnabled = false
        center.togglePlayPauseCommand.isEnabled = false
        center.stopCommand.isEnabled = false
        center.nextTrackCommand.isEnabled = false
        center.previousTrackCommand.isEnabled = false
        center.changePlaybackPositionCommand.isEnabled = false
    }

    private func emit(_ command: String, positionMs: Int64 = 0) {
        var data: [String: Any] = ["command": command]
        if command == "seek" {
            data["positionMs"] = positionMs
        }
        PluginHost.emit("media", "command", data)
    }
}
