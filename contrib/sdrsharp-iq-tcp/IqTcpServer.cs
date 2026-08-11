using System;
using System.Collections.Generic;
using System.Net;
using System.Net.Sockets;
using System.Threading;
using System.Threading.Channels;

namespace SDRSharp.IqTcpServer
{
    public sealed class IqTcpServer
    {
        private const int QueueCapacity = 64;

        private readonly object _sync = new object();
        private readonly List<Client> _clients = new List<Client>();
        private TcpListener _listener;
        private Thread _acceptThread;
        private volatile bool _running;

        public int Port { get; private set; }
        public bool Running => _running;

        public int ClientCount
        {
            get { lock (_sync) return _clients.Count; }
        }

        public event Action Changed;

        public void Start(int port)
        {
            Stop();
            _listener = new TcpListener(IPAddress.Any, port);
            _listener.Start();
            Port = port;
            _running = true;
            _acceptThread = new Thread(AcceptLoop) { IsBackground = true, Name = "IqTcp-Accept" };
            _acceptThread.Start();
            RaiseChanged();
        }

        public void Stop()
        {
            if (!_running && _listener == null) return;
            _running = false;
            try { _listener?.Stop(); } catch {  }
            _listener = null;
            lock (_sync)
            {
                foreach (var c in _clients) c.Dispose();
                _clients.Clear();
            }
            RaiseChanged();
        }

        private void AcceptLoop()
        {
            while (_running)
            {
                Socket sock;
                try
                {
                    sock = _listener.AcceptSocket();
                }
                catch
                {
                    break;
                }
                try { sock.NoDelay = true; } catch {  }

                var client = new Client(sock, QueueCapacity, OnClientClosed);
                lock (_sync) _clients.Add(client);
                client.Start();
                RaiseChanged();
            }
        }

        private void OnClientClosed(Client c)
        {
            bool removed;
            lock (_sync) removed = _clients.Remove(c);
            c.Dispose();
            if (removed) RaiseChanged();
        }

        public void Broadcast(byte[] block)
        {
            lock (_sync)
            {
                for (int i = 0; i < _clients.Count; i++)
                    _clients[i].Enqueue(block);
            }
        }

        private void RaiseChanged()
        {
            var h = Changed;
            if (h != null) h();
        }


        private sealed class Client
        {
            private readonly Socket _sock;
            private readonly Channel<byte[]> _channel;
            private readonly Thread _sender;
            private readonly Action<Client> _onClosed;
            private volatile bool _alive = true;

            public Client(Socket sock, int capacity, Action<Client> onClosed)
            {
                _sock = sock;
                _onClosed = onClosed;
                _channel = Channel.CreateBounded<byte[]>(new BoundedChannelOptions(capacity)
                {
                    FullMode = BoundedChannelFullMode.DropOldest,
                    SingleReader = true,
                    SingleWriter = false,
                });
                _sender = new Thread(SendLoop) { IsBackground = true, Name = "IqTcp-Send" };
            }

            public void Start() => _sender.Start();

            public void Enqueue(byte[] block)
            {
                if (_alive) _channel.Writer.TryWrite(block);
            }

            private void SendLoop()
            {
                var reader = _channel.Reader;
                try
                {
                    while (_alive)
                    {
                        byte[] block;
                        if (!reader.TryRead(out block))
                        {
                            if (!reader.WaitToReadAsync().AsTask().GetAwaiter().GetResult())
                                break;
                            continue;
                        }
                        int off = 0;
                        while (off < block.Length)
                        {
                            int sent = _sock.Send(block, off, block.Length - off, SocketFlags.None);
                            if (sent <= 0) throw new SocketException((int)SocketError.ConnectionReset);
                            off += sent;
                        }
                    }
                }
                catch
                {
                }
                finally
                {
                    _onClosed?.Invoke(this);
                }
            }

            public void Dispose()
            {
                if (!_alive) return;
                _alive = false;
                _channel.Writer.TryComplete();
                try { _sock.Shutdown(SocketShutdown.Both); } catch {  }
                try { _sock.Close(); } catch {  }
            }
        }
    }
}
