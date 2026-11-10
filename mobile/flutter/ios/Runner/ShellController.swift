import Flutter
import PhotosUI
import UIKit
import UniformTypeIdentifiers

/// The system tab bar over one full-screen Flutter view.
///
/// The tabs hold empty view controllers: the app is a single Flutter view that
/// swaps its page when told to. It sits below the bar so the bar's glass has
/// Flutter's pixels to sample, and it receives touches directly.
final class ShellController: UITabBarController, UITabBarControllerDelegate {
  private let flutter: FlutterViewController
  private let channel: FlutterMethodChannel
  private let tabCount = 4
  private var lastInsets: [String: Double] = [:]
  private let prefs = UserDefaults.standard
  private let prefsKey = "bowel.prefs"
  private let paperKey = "bowel.paper"
  private var darkChrome = false
  private var picked: FlutterResult?

  override var childForStatusBarStyle: UIViewController? { nil }
  override var preferredStatusBarStyle: UIStatusBarStyle { darkChrome ? .lightContent : .darkContent }

  init(flutter: FlutterViewController) {
    self.flutter = flutter
    channel = FlutterMethodChannel(name: "bowel/shell", binaryMessenger: flutter.binaryMessenger)
    super.init(nibName: nil, bundle: nil)
  }

  @available(*, unavailable)
  required init?(coder: NSCoder) { fatalError("ShellController is built in code") }

  override func viewDidLoad() {
    super.viewDidLoad()
    delegate = self
    viewControllers = [
      tab("代理", symbol: "gauge.with.needle.fill"),
      tab("活动", symbol: "waveform"),
      tab("Provider", symbol: "square.stack.3d.up.fill"),
      tab("设置", symbol: "gearshape.fill"),
    ]

    // Until Flutter's first frame, show the skin's paper and no bar, so a
    // first run doesn't flash a bar it's about to hide.
    tabBar.alpha = 0
    setBar(hidden: true)
    let paper = (prefs.object(forKey: paperKey) as? Int).map(UIColor.init(argb:)) ?? UIColor(named: "LaunchBackground") ?? .systemBackground
    view.backgroundColor = paper
    // Blank until the first frame, where Flutter starts drawing the logo.
    let splash = UIView()
    splash.backgroundColor = paper
    flutter.splashScreenView = splash

    addChild(flutter)
    flutter.view.frame = view.bounds
    flutter.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
    view.insertSubview(flutter.view, belowSubview: barHost)
    flutter.didMove(toParent: self)

    channel.setMethodCallHandler { [weak self] call, result in
      self?.handle(call, result: result)
    }
  }

  override func viewDidLayoutSubviews() {
    super.viewDidLayoutSubviews()
    if barIsOnTop {
      keepFlutterBelowBars()
    } else {
      view.bringSubviewToFront(barHost)
    }
    reportInsets()
  }

  /// On iPad the floating bar isn't inside `tabBar`, so `barHost` doesn't
  /// raise it. Directly above the empty pages' container, Flutter still gets
  /// the touches and every bar UIKit adds stays over it.
  private func keepFlutterBelowBars() {
    guard var host = selectedViewController?.view, host.isDescendant(of: view) else { return }
    while let parent = host.superview, parent !== view { host = parent }
    guard let at = view.subviews.firstIndex(of: host), view.subviews.firstIndex(of: flutter.view) != at + 1, host !== flutter.view else { return }
    view.insertSubview(flutter.view, aboveSubview: host)
  }

  override func viewSafeAreaInsetsDidChange() {
    super.viewSafeAreaInsetsDidChange()
    reportInsets()
  }

  /// UIKit doesn't carry safe areas down to a child that isn't one of the
  /// tabs, so Flutter is told them directly: the top inset, and how much of
  /// the bottom the bar covers.
  private var insets: [String: Double] {
    if barIsOnTop {
      // The floating bar isn't `tabBar`, but UIKit counts it in the safe area
      // of the tabs' own pages.
      let top = selectedViewController?.view.safeAreaInsets.top ?? view.safeAreaInsets.top
      return ["top": Double(max(top, topInset)), "bottom": Double(view.safeAreaInsets.bottom)]
    }
    let bottom: CGFloat
    if barIsHidden {
      bottom = view.safeAreaInsets.bottom
    } else {
      bottom = view.bounds.maxY - tabBar.convert(tabBar.bounds, to: view).minY
    }
    return ["top": Double(topInset), "bottom": Double(bottom)]
  }

  /// In a resizable iPad window this clears the window's own controls, which
  /// the plain safe area doesn't.
  private var topInset: CGFloat {
    if #available(iOS 26.0, *) { return view.edgeInsets(for: .safeArea(cornerAdaptation: .vertical)).top }
    return view.safeAreaInsets.top
  }

  /// An iPad window of regular width has the bar floating along the top.
  private var barIsOnTop: Bool {
    guard #available(iOS 18.0, *) else { return false }
    return traitCollection.userInterfaceIdiom == .pad && traitCollection.horizontalSizeClass == .regular
  }

  private var barIsHidden: Bool {
    if #available(iOS 18.0, *) { return isTabBarHidden }
    return tabBar.isHidden
  }

  /// `tabBar.isHidden` doesn't reach the floating bar on iPad.
  private func setBar(hidden: Bool) {
    if #available(iOS 18.0, *) {
      setTabBarHidden(hidden, animated: false)
    } else {
      tabBar.isHidden = hidden
    }
  }

  private func reportInsets() {
    let now = insets
    guard now != lastInsets else { return }
    lastInsets = now
    channel.invokeMethod("insets", arguments: now)
  }

  func tabBarController(_ tabBarController: UITabBarController, didSelect viewController: UIViewController) {
    channel.invokeMethod("tabSelected", arguments: selectedIndex)
  }

  /// The direct child of `view` that contains the bar.
  private var barHost: UIView {
    var host: UIView = tabBar
    while let parent = host.superview, parent !== view { host = parent }
    return host
  }

  private func tab(_ title: String, symbol: String) -> UIViewController {
    let page = UIViewController()
    page.view.backgroundColor = .clear
    page.tabBarItem = UITabBarItem(title: title, image: UIImage(systemName: symbol), selectedImage: nil)
    return page
  }

  private func handle(_ call: FlutterMethodCall, result: @escaping FlutterResult) {
    let args = call.arguments as? [String: Any] ?? [:]
    switch call.method {
    case "setTab":
      if let index = args["index"] as? Int, (0..<tabCount).contains(index) { selectedIndex = index }
      result(nil)
    case "setStyle":
      if let tint = args["tint"] as? Int {
        tabBar.tintColor = UIColor(argb: tint)
        // The floating bar on iPad takes its tint from here.
        view.tintColor = UIColor(argb: tint)
      }
      if let dark = args["dark"] as? Bool {
        // Only the bar: an override here would reach the Flutter child and
        // change the system brightness it reads, flipping the skin itself.
        darkChrome = dark
        tabBar.overrideUserInterfaceStyle = dark ? .dark : .light
        for bar in view.subviews where bar !== flutter.view { bar.overrideUserInterfaceStyle = dark ? .dark : .light }
        setNeedsStatusBarAppearanceUpdate()
      }
      if let paper = args["paper"] as? Int {
        prefs.set(paper, forKey: paperKey)
        view.backgroundColor = UIColor(argb: paper)
      }
      result(nil)
    case "setBadge":
      if let index = args["index"] as? Int, (0..<tabCount).contains(index),
        let item = viewControllers?[index].tabBarItem
      {
        // The floating bar draws no badge for an empty value.
        item.badgeValue = (args["on"] as? Bool) == true ? (barIsOnTop ? "!" : "") : nil
        if let color = args["color"] as? Int { item.badgeColor = UIColor(argb: color) }
      }
      result(nil)
    case "setTabTitles":
      if let titles = args["titles"] as? [String], let pages = viewControllers {
        for (page, title) in zip(pages, titles) { page.tabBarItem.title = title }
      }
      result(nil)
    case "setBarHidden":
      setBar(hidden: (args["hidden"] as? Bool) == true)
      tabBar.alpha = 1
      view.setNeedsLayout()
      result(nil)
    case "scanQR":
      guard presentedViewController == nil else {
        result(nil)
        return
      }
      let scanner = ScannerController(words: args as? [String: String] ?? [:]) { [weak self] code in
        self?.dismiss(animated: true) { result(code) }
      }
      scanner.modalPresentationStyle = .fullScreen
      present(scanner, animated: true)
    case "pickFile":
      guard presentedViewController == nil else {
        result(nil)
        return
      }
      picked = result
      // A copy, in this app's own temporary folder.
      let picker = UIDocumentPickerViewController(forOpeningContentTypes: [.item], asCopy: true)
      picker.delegate = self
      present(picker, animated: true)
    case "pickImage":
      guard presentedViewController == nil else {
        result(nil)
        return
      }
      picked = result
      var config = PHPickerConfiguration()
      config.filter = .images
      config.selectionLimit = 1
      let picker = PHPickerViewController(configuration: config)
      picker.delegate = self
      present(picker, animated: true)
    case "getInsets":
      result(insets)
    case "getPrefs":
      result(prefs.dictionary(forKey: prefsKey) ?? [:])
    case "setPref":
      if let key = args["key"] as? String, let value = args["value"] as? String {
        var all = prefs.dictionary(forKey: prefsKey) ?? [:]
        all[key] = value
        prefs.set(all, forKey: prefsKey)
      }
      result(nil)
    default:
      result(FlutterMethodNotImplemented)
    }
  }
}

extension ShellController: UIDocumentPickerDelegate {
  func documentPicker(_ controller: UIDocumentPickerViewController, didPickDocumentsAt urls: [URL]) {
    finishPick(urls.first?.path)
  }

  func documentPickerWasCancelled(_ controller: UIDocumentPickerViewController) {
    finishPick(nil)
  }

  fileprivate func finishPick(_ path: String?) {
    let done = picked
    picked = nil
    done?(path)
  }
}

extension ShellController: PHPickerViewControllerDelegate {
  func picker(_ picker: PHPickerViewController, didFinishPicking results: [PHPickerResult]) {
    dismiss(animated: true)
    guard let item = results.first?.itemProvider, item.canLoadObject(ofClass: UIImage.self) else {
      finishPick(nil)
      return
    }
    // As PNG: camera photos are HEIC, which the QR reader can't open.
    item.loadObject(ofClass: UIImage.self) { [weak self] object, _ in
      var path: String?
      if let data = (object as? UIImage)?.pngData() {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("\(UUID().uuidString).png")
        if (try? data.write(to: url)) != nil { path = url.path }
      }
      DispatchQueue.main.async { self?.finishPick(path) }
    }
  }
}

extension UIColor {
  fileprivate convenience init(argb: Int) {
    self.init(
      red: CGFloat((argb >> 16) & 0xFF) / 255,
      green: CGFloat((argb >> 8) & 0xFF) / 255,
      blue: CGFloat(argb & 0xFF) / 255,
      alpha: CGFloat((argb >> 24) & 0xFF) / 255)
  }
}
