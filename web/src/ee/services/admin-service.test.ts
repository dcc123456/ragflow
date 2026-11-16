import {
  createUser,
  deleteUser,
  listUserAgents,
  listUserDatasets,
} from './admin-service';

jest.mock('axios', () => {
  const request = {
    delete: jest.fn(),
    get: jest.fn(),
    post: jest.fn(),
    put: jest.fn(),
    interceptors: {
      request: { use: jest.fn() },
      response: { use: jest.fn() },
    },
  };

  return {
    __esModule: true,
    default: { create: () => request },
    mockRequest: request,
  };
});

const { mockRequest } = jest.requireMock('axios');

describe('createUser', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('rejects when creation returns a business error', async () => {
    mockRequest.post.mockResolvedValue({
      data: { code: 500, message: 'user already exists' },
    });

    await expect(createUser('user@example.com', 'password')).rejects.toThrow(
      'user already exists',
    );
  });
});

describe('deleteUser', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('deactivates the user before deleting them', async () => {
    mockRequest.put.mockResolvedValue({ data: { code: 0 } });
    mockRequest.delete.mockResolvedValue({ data: { code: 0 } });

    await deleteUser('user@example.com');

    expect(mockRequest.put).toHaveBeenCalledWith(
      '/api/v1/admin/users/user@example.com/activate',
      { activate_status: 'off' },
    );
    expect(mockRequest.delete).toHaveBeenCalledWith(
      '/api/v1/admin/users/user@example.com',
    );
    expect(mockRequest.put.mock.invocationCallOrder[0]).toBeLessThan(
      mockRequest.delete.mock.invocationCallOrder[0],
    );
  });

  it('does not delete when deactivation returns a business error', async () => {
    mockRequest.put.mockResolvedValue({
      data: { code: 500, message: 'deactivation failed' },
    });

    await expect(deleteUser('user@example.com')).rejects.toThrow(
      'deactivation failed',
    );
    expect(mockRequest.delete).not.toHaveBeenCalled();
  });

  it('rejects when deletion returns a business error', async () => {
    mockRequest.put.mockResolvedValue({ data: { code: 0 } });
    mockRequest.delete.mockResolvedValue({
      data: { code: 500, message: 'deletion failed' },
    });

    await expect(deleteUser('user@example.com')).rejects.toThrow(
      'deletion failed',
    );
  });
});

describe('user asset lists', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockRequest.get.mockResolvedValue({
      data: { code: 0, data: [], total: 0 },
    });
  });

  it('passes Dataset pagination to the admin API', async () => {
    await listUserDatasets('user@example.com', { page: 2, page_size: 20 });

    expect(mockRequest.get).toHaveBeenCalledWith(
      '/api/v1/admin/users/user@example.com/datasets',
      { params: { page: 2, page_size: 20 } },
    );
  });

  it.each(['agent_canvas', 'dataflow_canvas'] as const)(
    'passes %s filtering and pagination to the admin API',
    async (canvasCategory) => {
      await listUserAgents('user@example.com', {
        canvas_category: canvasCategory,
        page: 3,
        page_size: 50,
      });

      expect(mockRequest.get).toHaveBeenCalledWith(
        '/api/v1/admin/users/user@example.com/agents',
        {
          params: {
            canvas_category: canvasCategory,
            page: 3,
            page_size: 50,
          },
        },
      );
    },
  );
});
