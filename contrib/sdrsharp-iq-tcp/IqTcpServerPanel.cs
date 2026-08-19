using System;
using System.Drawing;
using System.Globalization;
using System.Windows.Forms;

namespace SDRSharp.IqTcpServer
{
    public class IqTcpServerPanel : UserControl
    {
        private const int PollIntervalMs = 200;

        private readonly IqTcpServerPlugin _plugin;
        private readonly IqTcpServer _server;
        private readonly IqDdc _ddc;

        private readonly NumericUpDown _portBox;
        private readonly CheckBox _followBox;
        private readonly CheckBox _decimateBox;
        private readonly Button _toggleButton;
        private readonly Label _statusLabel;
        private readonly Label _rateLabel;
        private readonly Label _vfoLabel;
        private readonly Label _levelLabel;
        private readonly TextBox _commandBox;
        private readonly Timer _timer;
        private readonly System.Diagnostics.Stopwatch _clock = System.Diagnostics.Stopwatch.StartNew();

        private double _sampleRate;
        private long _lastSampleCount;
        private double _lastMeasureAt;
        private double _measuredRate;

        public IqTcpServerPanel(IqTcpServerPlugin plugin)
        {
            _plugin = plugin;
            _server = plugin.Server;
            _ddc = plugin.Ddc;

            var layout = new TableLayoutPanel
            {
                Dock = DockStyle.Fill,
                ColumnCount = 2,
                AutoSize = true,
                Padding = new Padding(4),
            };
            layout.ColumnStyles.Add(new ColumnStyle(SizeType.AutoSize));
            layout.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100f));

            int row = 0;
            layout.Controls.Add(new Label
            {
                Text = "Port",
                AutoSize = true,
                Anchor = AnchorStyles.Left,
                TextAlign = ContentAlignment.MiddleLeft,
            }, 0, row);
            _portBox = new NumericUpDown
            {
                Minimum = 1,
                Maximum = 65535,
                Value = 5555,
                Dock = DockStyle.Fill,
            };
            _portBox.ValueChanged += (s, e) => UpdateCommand();
            layout.Controls.Add(_portBox, 1, row++);

            _followBox = new CheckBox
            {
                Text = "Follow VFO (DDC → 0 Hz)",
                Checked = _ddc.FollowVfo,
                AutoSize = true,
                Anchor = AnchorStyles.Left,
            };
            _followBox.CheckedChanged += OnModeChanged;
            row = AddSpanned(layout, _followBox, row);

            _decimateBox = new CheckBox
            {
                Text = "Decimate stream (saves CPU)",
                Checked = _ddc.Decimate,
                AutoSize = true,
                Anchor = AnchorStyles.Left,
            };
            _decimateBox.CheckedChanged += OnModeChanged;
            row = AddSpanned(layout, _decimateBox, row);

            _toggleButton = new Button { Text = "Start server", Dock = DockStyle.Fill };
            _toggleButton.Click += OnToggle;
            row = AddSpanned(layout, _toggleButton, row);

            _statusLabel = new Label { Text = "Stopped", AutoSize = true, Anchor = AnchorStyles.Left };
            row = AddSpanned(layout, _statusLabel, row);

            _rateLabel = new Label { Text = "Rate: (start the radio)", AutoSize = true, Anchor = AnchorStyles.Left };
            row = AddSpanned(layout, _rateLabel, row);

            _vfoLabel = new Label { Text = "VFO offset: —", AutoSize = true, Anchor = AnchorStyles.Left };
            row = AddSpanned(layout, _vfoLabel, row);

            _levelLabel = new Label { Text = "Level: —", AutoSize = true, Anchor = AnchorStyles.Left };
            row = AddSpanned(layout, _levelLabel, row);

            _commandBox = new TextBox
            {
                ReadOnly = true,
                Multiline = true,
                Height = 46,
                Dock = DockStyle.Fill,
                Text = string.Empty,
            };
            row = AddSpanned(layout, _commandBox, row);

            layout.RowCount = row;
            Controls.Add(layout);
            AutoSize = true;

            _server.Changed += OnServerChanged;
            _timer = new Timer { Interval = PollIntervalMs };
            _timer.Tick += OnTick;
            _timer.Start();
            UpdateStatus();
            UpdateCommand();
        }

        private static int AddSpanned(TableLayoutPanel layout, Control control, int row)
        {
            layout.Controls.Add(control, 0, row);
            layout.SetColumnSpan(control, 2);
            return row + 1;
        }

        private void OnToggle(object sender, EventArgs e)
        {
            if (_server.Running)
            {
                _plugin.Enabled = false;
                _server.Stop();
            }
            else
            {
                _plugin.SyncVfoOffset();
                _server.Start((int)_portBox.Value);
                _plugin.Enabled = true;
            }
            UpdateStatus();
            UpdateCommand();
        }

        private void OnModeChanged(object sender, EventArgs e)
        {
            _ddc.FollowVfo = _followBox.Checked;
            _ddc.Decimate = _decimateBox.Checked;
            _plugin.SyncVfoOffset();
            UpdateStatus();
            UpdateCommand();
        }

        public void SetSampleRate(double sampleRate)
        {
            RunOnUi(() =>
            {
                _sampleRate = sampleRate;
                UpdateRate();
                UpdateCommand();
            });
        }

        private void OnTick(object sender, EventArgs e)
        {
            double offset = _plugin.SyncVfoOffset();
            _vfoLabel.Text = _ddc.FollowVfo
                ? string.Format(CultureInfo.InvariantCulture,
                                "VFO offset: {0:+0.000;-0.000;0.000} kHz → 0 Hz", offset / 1e3)
                : "VFO offset: (not followed — raw I/Q)";

            double db = _ddc.TakeLevelDbfs();
            _levelLabel.Text = double.IsNaN(db)
                ? "Level: — (not streaming)"
                : string.Format(CultureInfo.InvariantCulture, "Level: {0:0.0} dBFS", db);

            MeasureRate();
        }

        private void MeasureRate()
        {
            double now = _clock.Elapsed.TotalSeconds;
            long count = _ddc.OutputSampleCount;
            double elapsed = now - _lastMeasureAt;
            if (elapsed < 1.0) return;
            long delta = count - _lastSampleCount;
            _lastMeasureAt = now;
            _lastSampleCount = count;
            _measuredRate = delta > 0 ? delta / elapsed : 0.0;
            UpdateRate();
        }

        private void OnServerChanged()
        {
            RunOnUi(UpdateStatus);
        }

        private void UpdateStatus()
        {
            bool running = _server.Running;
            _toggleButton.Text = running ? "Stop server" : "Start server";
            _portBox.Enabled = !running;
            _followBox.Enabled = !running;
            _decimateBox.Enabled = !running && _followBox.Checked;
            _statusLabel.Text = running
                ? string.Format("Listening on 0.0.0.0:{0} — {1} client(s)", _server.Port, _server.ClientCount)
                : "Stopped";
            UpdateRate();
        }

        private void UpdateRate()
        {
            if (_sampleRate <= 0)
            {
                _rateLabel.Text = "Rate: (start the radio)";
                return;
            }
            int ratio = _ddc.DecimationRatio;
            string text = ratio > 1
                ? string.Format(CultureInfo.InvariantCulture,
                                "Rate: {0:#,0} Hz / {1} → {2:#,0.##} Hz", _sampleRate, ratio,
                                _ddc.OutputSampleRate)
                : string.Format(CultureInfo.InvariantCulture, "Rate: {0:#,0} Hz (no decimation)", _sampleRate);
            if (_measuredRate > 0.0)
            {
                text += string.Format(CultureInfo.InvariantCulture,
                                      "\r\nMeasured: {0:#,0} Hz", _measuredRate);
            }
            _rateLabel.Text = text;
        }

        private void UpdateCommand()
        {
            double fs = _sampleRate > 0 ? _ddc.OutputSampleRate : 0.0;
            string fsText = fs > 0
                ? fs.ToString("0.##", CultureInfo.InvariantCulture)
                : "<start the radio>";
            _commandBox.Text = string.Format(CultureInfo.InvariantCulture,
                "std-t86 live tcp://127.0.0.1:{0} --offset 0 --fs {1} --fmt cf32",
                (int)_portBox.Value, fsText);
        }

        private void RunOnUi(Action action)
        {
            if (IsDisposed || Disposing) return;
            try
            {
                if (InvokeRequired) BeginInvoke(action);
                else action();
            }
            catch (ObjectDisposedException) {  }
            catch (InvalidOperationException) {  }
        }

        protected override void Dispose(bool disposing)
        {
            if (disposing)
            {
                _timer.Stop();
                _timer.Dispose();
                _server.Changed -= OnServerChanged;
                _server.Stop();
            }
            base.Dispose(disposing);
        }
    }
}
